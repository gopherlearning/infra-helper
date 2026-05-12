package relocator

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Poster delivers extracted JSON payloads to the configured ingest URL.
type Poster struct {
	cfg    PostConfig
	client *http.Client
}

// NewPoster wires the HTTP client and config for the JSON sink.
func NewPoster(cfg PostConfig) *Poster {
	return &Poster{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}
}

// Send posts one JSON document with retries and optional HMAC signing.
//
// Returns nil on a 2xx response. Non-retryable client errors (4xx other than
// 408/429) abort immediately; 5xx and transport errors are retried.
func (p *Poster) Send(ctx context.Context, sourceName, fileName string, body []byte) error {
	if p.cfg.URL == "" {
		return errPostNotConfigured
	}

	retries := max(p.cfg.Retries, 0)

	var lastErr error

	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			waitErr := sleepWithCtx(ctx, p.cfg.RetryBackoff)
			if waitErr != nil {
				return waitErr
			}
		}

		sendErr := p.doSend(ctx, sourceName, fileName, body)
		if sendErr == nil {
			return nil
		}

		lastErr = sendErr

		if !isRetryable(sendErr) {
			return sendErr
		}
	}

	return fmt.Errorf("post exhausted retries: %w", lastErr)
}

func (p *Poster) doSend(ctx context.Context, sourceName, fileName string, body []byte) error {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.URL, bytes.NewReader(body))
	if reqErr != nil {
		return fmt.Errorf("build request: %w", reqErr)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relocator-Source", sourceName)
	req.Header.Set("X-Relocator-File", fileName)

	for k, v := range p.cfg.Headers {
		req.Header.Set(k, v)
	}

	if p.cfg.HMACSecret != "" {
		mac := hmac.New(sha256.New, []byte(p.cfg.HMACSecret))
		_, _ = mac.Write(body)
		req.Header.Set(p.cfg.HMACHeader, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, doErr := p.client.Do(req)
	if doErr != nil {
		return &retryableError{err: doErr}
	}

	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	statusErr := fmt.Errorf("%w: %d", errBadStatus, resp.StatusCode)
	if resp.StatusCode >= http.StatusInternalServerError ||
		resp.StatusCode == http.StatusRequestTimeout ||
		resp.StatusCode == http.StatusTooManyRequests {
		return &retryableError{err: statusErr}
	}

	return statusErr
}

func sleepWithCtx(ctx context.Context, dur time.Duration) error {
	if dur <= 0 {
		return nil
	}

	timer := time.NewTimer(dur)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return fmt.Errorf("context cancelled: %w", ctxErr)
		}

		return nil
	case <-timer.C:
		return nil
	}
}

type retryableError struct {
	err error
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var re *retryableError

	return errors.As(err, &re)
}

var (
	errPostNotConfigured = errors.New("post target is not configured")
	errBadStatus         = errors.New("non-2xx http status")
)
