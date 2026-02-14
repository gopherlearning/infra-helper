package listupdater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	tmpFilePerm    os.FileMode = 0o644
	tmpDirPerm     os.FileMode = 0o750
	userAgent                  = "infra-helper/list-updater"
	requestTimeout             = 60 * time.Second
)

var (
	errAuthDataStringRequired = errors.New("authData must be a string")
	errAuthBasicFormat        = errors.New("basic authData must be user:password")
	errUnsupportedAuthType    = errors.New("unsupported authType")
	errUnexpectedHTTPStatus   = errors.New("unexpected HTTP status")
)

func (s *service) syncAll(ctx context.Context) error {
	mkdirOrigErr := os.MkdirAll(s.origDir, tmpDirPerm)
	if mkdirOrigErr != nil {
		return fmt.Errorf("mkdir original dir: %w", mkdirOrigErr)
	}

	mkdirPlainErr := os.MkdirAll(s.plainDir, tmpDirPerm)
	if mkdirPlainErr != nil {
		return fmt.Errorf("mkdir plain dir: %w", mkdirPlainErr)
	}

	for _, lst := range s.lists {
		syncErr := s.syncOne(ctx, lst)
		if syncErr != nil {
			log.Error().Err(syncErr).Str("name", lst.Name).Str("url", lst.URL).Msg("sync list failed")
		}
	}

	return nil
}

func (s *service) syncOne(ctx context.Context, lst list) error {
	origPath := filepath.Join(s.origDir, lst.Name)
	plainPath := filepath.Join(s.plainDir, lst.Name)

	dlCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	downloadErr := s.downloadToFile(dlCtx, lst, origPath)
	if downloadErr != nil {
		return downloadErr
	}

	plainErr := ensurePlain(origPath, plainPath)
	if plainErr != nil {
		return fmt.Errorf("ensure plain: %w", plainErr)
	}

	return nil
}

func (s *service) downloadToFile(ctx context.Context, lst list, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lst.URL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	authErr := applyAuth(req, lst)
	if authErr != nil {
		return authErr
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: %s", errUnexpectedHTTPStatus, resp.Status)
	}

	tmpPath := destPath + ".tmp"
	//nolint:gosec // file path is derived from trusted config (no user input).
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, tmpFilePerm)
	if err != nil {
		return fmt.Errorf("open tmp file: %w", err)
	}

	copyErr := func() error {
		_, err = io.Copy(file, resp.Body)
		if err != nil {
			return fmt.Errorf("write tmp file: %w", err)
		}

		return nil
	}()

	closeErr := file.Close()

	if copyErr != nil {
		_ = os.Remove(tmpPath)

		return copyErr
	}

	if closeErr != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("close tmp file: %w", closeErr)
	}

	renameErr := os.Rename(tmpPath, destPath)
	if renameErr != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("rename tmp file: %w", renameErr)
	}

	return nil
}

func applyAuth(req *http.Request, lst list) error {
	// Auth (optional).
	switch lst.AuthType {
	case "":
		return nil
	case "basic":
		// authData is expected as "user:pass".
		v, isString := lst.AuthData.(string)
		if !isString {
			return fmt.Errorf("%w: basic", errAuthDataStringRequired)
		}

		user, pass, ok := splitUserPass(v)
		if !ok {
			return errAuthBasicFormat
		}

		req.SetBasicAuth(user, pass)

		return nil
	case "token":
		v, isString := lst.AuthData.(string)
		if !isString {
			return fmt.Errorf("%w: token", errAuthDataStringRequired)
		}

		req.Header.Set("Authorization", "Bearer "+v)

		return nil
	default:
		return fmt.Errorf("%w: %s", errUnsupportedAuthType, lst.AuthType)
	}
}

func splitUserPass(s string) (string, string, bool) {
	for i := range len(s) {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}

	return "", "", false
}
