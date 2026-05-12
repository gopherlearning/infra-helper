package relocator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/rs/zerolog/log"
)

const (
	tmpDirPerm    = 0o755
	tmpFilePerm   = 0o600
	downloadBufKB = 64
)

// handleObject runs the full pipeline for a single object: dedup → download →
// extract → POST → optional delete. Each stage updates stats and emits events
// that surface on the status page.
func (s *Service) handleObject(ctx context.Context, runtime *bucketRuntime, obj minio.ObjectInfo) error {
	processed, dedupErr := s.store.IsProcessed(runtime.cfg.Name, obj.Key, obj.ETag)
	if dedupErr != nil {
		return fmt.Errorf("dedup lookup: %w", dedupErr)
	}

	if processed {
		_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) { st.Skipped++ })

		return nil
	}

	if runtime.cfg.MaxObjectSize > 0 && obj.Size > runtime.cfg.MaxObjectSize {
		s.recordEvent(Event{
			Level: "warn", Bucket: runtime.cfg.Name, Object: obj.Key,
			Message: fmt.Sprintf("object size %d > max_size %d", obj.Size, runtime.cfg.MaxObjectSize),
		})
		_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) { st.Skipped++ })

		return nil
	}

	runtime.busy.Add(1)
	s.metrics.InFlight.WithLabelValues(runtime.cfg.Name).Inc()

	defer func() {
		runtime.busy.Add(-1)
		s.metrics.InFlight.WithLabelValues(runtime.cfg.Name).Dec()
	}()

	return s.processObject(ctx, runtime, obj)
}

func (s *Service) processObject(ctx context.Context, runtime *bucketRuntime, obj minio.ObjectInfo) error {
	localPath, downloadErr := s.downloadObject(ctx, runtime, obj)
	if downloadErr != nil {
		s.metrics.Objects.WithLabelValues(runtime.cfg.Name, "download_failed").Inc()
		_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) {
			st.DownloadFailed++
			st.LastError = downloadErr.Error()
		})
		s.recordEvent(Event{
			Level: "error", Bucket: runtime.cfg.Name, Object: obj.Key,
			Message: "download: " + downloadErr.Error(),
		})

		return downloadErr
	}

	defer func() { _ = os.Remove(localPath) }()

	s.metrics.Objects.WithLabelValues(runtime.cfg.Name, "downloaded").Inc()
	s.metrics.Bytes.WithLabelValues(runtime.cfg.Name).Add(float64(obj.Size))
	_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) {
		st.Downloaded++

		if obj.Size > 0 {
			st.BytesDownloaded += uint64(obj.Size)
		}
	})

	files, extractErr := ExtractJSON(localPath, s.cfg.Passwords)
	if extractErr != nil {
		s.handleExtractError(runtime, obj, localPath, extractErr)

		return extractErr
	}

	s.metrics.Objects.WithLabelValues(runtime.cfg.Name, "extracted").Inc()
	_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) { st.Extracted++ })

	posted, postFailed := s.sendFiles(ctx, runtime.cfg.Name, obj.Key, files)

	_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) {
		st.Posted += uint64(posted)         //nolint:gosec // counter values are non-negative
		st.PostFailed += uint64(postFailed) //nolint:gosec // counter values are non-negative
	})

	if postFailed > 0 {
		s.recordEvent(Event{
			Level: "warn", Bucket: runtime.cfg.Name, Object: obj.Key,
			Message: fmt.Sprintf("posted=%d failed=%d", posted, postFailed),
		})
	}

	storeErr := s.store.MarkProcessed(runtime.cfg.Name, obj.Key, ProcessedEntry{
		ETag:        obj.ETag,
		Size:        obj.Size,
		Status:      processedStatus(postFailed),
		Files:       len(files),
		ProcessedAt: time.Now().UTC(),
	})
	if storeErr != nil {
		log.Error().Err(storeErr).Msg("mark processed")
	}

	if postFailed == 0 && runtime.cfg.DeleteAfter {
		removeErr := runtime.client.RemoveObject(ctx, runtime.cfg.Bucket, obj.Key, minio.RemoveObjectOptions{})
		if removeErr != nil {
			log.Error().Err(removeErr).Str("object", obj.Key).Msg("delete after success failed")
			s.recordEvent(Event{
				Level: "warn", Bucket: runtime.cfg.Name, Object: obj.Key,
				Message: "remote delete failed: " + removeErr.Error(),
			})
		} else {
			_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) { st.Deleted++ })
			s.recordEvent(Event{Level: "info", Bucket: runtime.cfg.Name, Object: obj.Key, Message: "deleted from source"})
		}
	}

	return nil
}

func processedStatus(postFailed int) string {
	if postFailed > 0 {
		return "partial"
	}

	return "ok"
}

func (s *Service) handleExtractError(runtime *bucketRuntime, obj minio.ObjectInfo, localPath string, err error) {
	if errors.Is(err, errPasswordRequired) {
		s.metrics.Objects.WithLabelValues(runtime.cfg.Name, "password_failed").Inc()
		_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) {
			st.PasswordFailed++
			st.LastError = err.Error()
		})
	} else {
		s.metrics.Objects.WithLabelValues(runtime.cfg.Name, "extract_failed").Inc()
		_ = s.store.UpdateStats(runtime.cfg.Name, func(st *BucketStats) {
			st.ExtractFailed++
			st.LastError = err.Error()
		})
	}

	s.recordEvent(Event{Level: "error", Bucket: runtime.cfg.Name, Object: obj.Key, Message: "extract: " + err.Error()})

	s.quarantine(localPath, obj.Key)
}

func (s *Service) sendFiles(ctx context.Context, sourceName, objectKey string, files []JSONFile) (int, int) {
	var posted, failed int

	for _, file := range files {
		sendErr := s.poster.Send(ctx, sourceName, file.Name, file.Body)
		if sendErr != nil {
			failed++

			s.metrics.Files.WithLabelValues(sourceName, "failed").Inc()
			log.Warn().Err(sendErr).Str("object", objectKey).Str("file", file.Name).Msg("post failed")

			continue
		}

		posted++

		s.metrics.Files.WithLabelValues(sourceName, "ok").Inc()
	}

	return posted, failed
}

func (s *Service) downloadObject(ctx context.Context, runtime *bucketRuntime, obj minio.ObjectInfo) (string, error) {
	mkdirErr := os.MkdirAll(s.cfg.WorkDir, tmpDirPerm)
	if mkdirErr != nil {
		return "", fmt.Errorf("mkdir workdir: %w", mkdirErr)
	}

	target := filepath.Clean(filepath.Join(s.cfg.WorkDir, sanitizeFileName(runtime.cfg.Name+"-"+obj.Key)))

	out, createErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, tmpFilePerm)
	if createErr != nil {
		return "", fmt.Errorf("open temp file: %w", createErr)
	}

	defer func() { _ = out.Close() }()

	src, getErr := runtime.client.GetObject(ctx, runtime.cfg.Bucket, obj.Key, minio.GetObjectOptions{})
	if getErr != nil {
		_ = os.Remove(target)

		return "", fmt.Errorf("get object: %w", getErr)
	}

	defer func() { _ = src.Close() }()

	_, copyErr := io.Copy(out, src)
	if copyErr != nil {
		_ = os.Remove(target)

		return "", fmt.Errorf("copy stream: %w", copyErr)
	}

	return target, nil
}

func (s *Service) quarantine(srcPath, objectKey string) {
	if s.cfg.QuarantineDir == "" {
		return
	}

	mkdirErr := os.MkdirAll(s.cfg.QuarantineDir, tmpDirPerm)
	if mkdirErr != nil {
		log.Error().Err(mkdirErr).Msg("mkdir quarantine")

		return
	}

	dest := filepath.Join(s.cfg.QuarantineDir,
		fmt.Sprintf("%d-%s", time.Now().Unix(), sanitizeFileName(objectKey)))

	renameErr := os.Rename(srcPath, dest)
	if renameErr != nil {
		log.Warn().Err(renameErr).Msg("quarantine move failed")
	}
}

func sanitizeFileName(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", "..", "_", ":", "_")

	return r.Replace(name)
}
