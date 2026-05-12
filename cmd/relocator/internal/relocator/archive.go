package relocator

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pwzip "github.com/yeka/zip"
)

const (
	maxJSONFileSize = 64 * 1024 * 1024 // 64 MiB safety cap per extracted file
)

var (
	errUnsupportedArchive = errors.New("unsupported archive format")
	errPasswordRequired   = errors.New("archive is encrypted and no password matched")
	errEntryTooLarge      = errors.New("archive entry exceeds size cap")
)

// JSONFile is a single JSON entry extracted from an archive.
type JSONFile struct {
	Name string
	Body []byte
}

type readCloser struct {
	io.Reader

	closers []io.Closer
}

func (r *readCloser) Close() error {
	var first error

	for _, closer := range r.closers {
		closeErr := closer.Close()
		if closeErr != nil && first == nil {
			first = closeErr
		}
	}

	if first != nil {
		return fmt.Errorf("close chain: %w", first)
	}

	return nil
}

func openCompressed(path, compression string) (io.ReadCloser, error) {
	file, openErr := os.Open(filepath.Clean(path))
	if openErr != nil {
		return nil, fmt.Errorf("open archive: %w", openErr)
	}

	switch compression {
	case "":
		return &readCloser{Reader: file, closers: []io.Closer{file}}, nil
	case "gzip":
		gzReader, gzErr := gzip.NewReader(file)
		if gzErr != nil {
			_ = file.Close()

			return nil, fmt.Errorf("gzip reader: %w", gzErr)
		}

		return &readCloser{Reader: gzReader, closers: []io.Closer{gzReader, file}}, nil
	case "bzip2":
		bzReader := bzip2.NewReader(file)

		return &readCloser{Reader: bzReader, closers: []io.Closer{file}}, nil
	default:
		_ = file.Close()

		return nil, fmt.Errorf("%w: %s", errUnsupportedArchive, compression)
	}
}

// ExtractJSON inspects the archive at path, tries the password list when
// needed, and returns every JSON file it can decode. The set of passwords may
// be empty for non-encrypted archives.
func ExtractJSON(path string, passwords []string) ([]JSONFile, error) {
	switch {
	case strings.HasSuffix(strings.ToLower(path), ".zip"):
		return extractZip(path, passwords)
	case strings.HasSuffix(strings.ToLower(path), ".tar"):
		return extractTar(path, "")
	case strings.HasSuffix(strings.ToLower(path), ".tar.gz"),
		strings.HasSuffix(strings.ToLower(path), ".tgz"):
		return extractTar(path, "gzip")
	case strings.HasSuffix(strings.ToLower(path), ".tar.bz2"),
		strings.HasSuffix(strings.ToLower(path), ".tbz2"):
		return extractTar(path, "bzip2")
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedArchive, filepath.Ext(path))
	}
}

func extractZip(path string, passwords []string) ([]JSONFile, error) {
	reader, openErr := pwzip.OpenReader(path)
	if openErr != nil {
		return nil, fmt.Errorf("open zip: %w", openErr)
	}

	defer func() { _ = reader.Close() }()

	out := make([]JSONFile, 0, len(reader.File))

	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}

		if !strings.HasSuffix(strings.ToLower(entry.Name), ".json") {
			continue
		}

		body, readErr := readZipEntry(entry, passwords)
		if readErr != nil {
			return nil, readErr
		}

		out = append(out, JSONFile{Name: entry.Name, Body: body})
	}

	return out, nil
}

func readZipEntry(entry *pwzip.File, passwords []string) ([]byte, error) {
	if !entry.IsEncrypted() {
		return readZipReader(entry)
	}

	for _, pwd := range passwords {
		entry.SetPassword(pwd)

		body, readErr := readZipReader(entry)
		if readErr == nil {
			return body, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", errPasswordRequired, entry.Name)
}

func readZipReader(entry *pwzip.File) ([]byte, error) {
	rdr, openErr := entry.Open()
	if openErr != nil {
		return nil, fmt.Errorf("open zip entry: %w", openErr)
	}

	defer func() { _ = rdr.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(rdr, maxJSONFileSize+1))
	if readErr != nil {
		return nil, fmt.Errorf("read zip entry: %w", readErr)
	}

	if int64(len(body)) > maxJSONFileSize {
		return nil, fmt.Errorf("%w: %s", errEntryTooLarge, entry.Name)
	}

	return body, nil
}

func extractTar(path, compression string) ([]JSONFile, error) {
	file, openErr := openCompressed(path, compression)
	if openErr != nil {
		return nil, openErr
	}

	defer func() { _ = file.Close() }()

	tarReader := tar.NewReader(file)
	out := make([]JSONFile, 0)

	for {
		hdr, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}

		if nextErr != nil {
			return nil, fmt.Errorf("tar next: %w", nextErr)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		if !strings.HasSuffix(strings.ToLower(hdr.Name), ".json") {
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(tarReader, maxJSONFileSize+1))
		if readErr != nil {
			return nil, fmt.Errorf("read tar entry: %w", readErr)
		}

		if int64(len(body)) > maxJSONFileSize {
			return nil, fmt.Errorf("%w: %s", errEntryTooLarge, hdr.Name)
		}

		out = append(out, JSONFile{Name: hdr.Name, Body: body})
	}

	return out, nil
}
