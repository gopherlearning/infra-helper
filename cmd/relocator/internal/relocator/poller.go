package relocator

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog/log"
)

// bucketRuntime holds the per-bucket state used by the poller.
type bucketRuntime struct {
	cfg     Bucket
	client  *minio.Client
	trigger chan struct{}
	busy    atomic.Int32
}

func newBucketRuntime(cfg Bucket) (*bucketRuntime, error) {
	client, clientErr := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if clientErr != nil {
		return nil, fmt.Errorf("create s3 client: %w", clientErr)
	}

	return &bucketRuntime{
		cfg:     cfg,
		client:  client,
		trigger: make(chan struct{}, 1),
	}, nil
}

// runPoller drives one bucket: periodic ticks, manual triggers, cancellation.
//
// The lifecycle ctx is observed indirectly through newPollerJob, which derives
// its own context from the global app lifecycle.
func (s *Service) runPoller(_ context.Context, runtime *bucketRuntime) {
	jobCtx, onStop := newPollerJob(runtime.cfg.Name)
	defer onStop()

	interval := runtime.cfg.PollInterval
	if interval <= 0 {
		interval = s.cfg.PollInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.pollOnce(jobCtx, runtime) //nolint:contextcheck // jobCtx descends from the global app context

	for {
		select {
		case <-jobCtx.Done():
			return
		case <-ticker.C:
			s.pollOnce(jobCtx, runtime) //nolint:contextcheck // jobCtx descends from the global app context
		case <-runtime.trigger:
			s.pollOnce(jobCtx, runtime) //nolint:contextcheck // jobCtx descends from the global app context
		}
	}
}

func (s *Service) pollOnce(ctx context.Context, runtime *bucketRuntime) {
	timer := time.Now()

	defer func() {
		s.metrics.PollDuration.WithLabelValues(runtime.cfg.Name).Observe(time.Since(timer).Seconds())
	}()

	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	opts := minio.ListObjectsOptions{
		Prefix:    runtime.cfg.Prefix,
		Recursive: true,
	}

	for obj := range runtime.client.ListObjects(listCtx, runtime.cfg.Bucket, opts) {
		if obj.Err != nil {
			log.Error().Err(obj.Err).Str("bucket", runtime.cfg.Name).Msg("list objects failed")
			s.recordEvent(Event{Level: "error", Bucket: runtime.cfg.Name, Message: "list: " + obj.Err.Error()})

			break
		}

		if !objectMatches(obj.Key, runtime.cfg) {
			continue
		}

		processErr := s.handleObject(ctx, runtime, obj)
		if processErr != nil && !errors.Is(processErr, context.Canceled) {
			log.Error().Err(processErr).
				Str("bucket", runtime.cfg.Name).
				Str("object", obj.Key).
				Msg("object pipeline failed")
		}
	}
}

func objectMatches(key string, cfg Bucket) bool {
	if cfg.MaxObjectSize > 0 && cfg.ObjectPattern == "" {
		return true
	}

	if cfg.ObjectPattern == "" {
		return true
	}

	matched, matchErr := path.Match(cfg.ObjectPattern, path.Base(key))
	if matchErr != nil {
		return false
	}

	return matched
}
