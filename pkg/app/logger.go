package app

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	maxLogURIBytes = 2048
	ellipsisLen    = 3
)

// EchoZerologMiddleware logs HTTP requests via zerolog.
func EchoZerologMiddleware() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		HandleError:     true,
		LogLatency:      true,
		LogMethod:       true,
		LogURI:          true,
		LogRoutePath:    true,
		LogStatus:       true,
		LogRemoteIP:     true,
		LogUserAgent:    true,
		LogRequestID:    true,
		LogResponseSize: true,
		LogValuesFunc: func(_ *echo.Context, values middleware.RequestLoggerValues) error {
			uri := truncateForLog(values.URI, maxLogURIBytes)

			evt := log.WithLevel(levelForStatus(values.Status))
			evt = evt.
				Str("method", values.Method).
				Str("uri", uri).
				Int("status", values.Status).
				Dur("latency", values.Latency).
				Str("remote_ip", values.RemoteIP).
				Str("user_agent", values.UserAgent).
				Int64("bytes", values.ResponseSize)

			if values.RoutePath != "" {
				evt = evt.Str("route", values.RoutePath)
			}

			if values.RequestID != "" {
				evt = evt.Str("request_id", values.RequestID)
			}

			if values.Error != nil {
				evt = evt.Err(values.Error)
			}

			evt.Msg("http request")

			return nil
		},
	})
}

func levelForStatus(status int) zerolog.Level {
	if status >= http.StatusInternalServerError {
		return zerolog.ErrorLevel
	}

	if status >= http.StatusBadRequest {
		return zerolog.WarnLevel
	}

	return zerolog.InfoLevel
}

func truncateForLog(str string, maxBytes int) string {
	if len(str) <= maxBytes {
		return str
	}

	if maxBytes <= ellipsisLen {
		return str[:maxBytes]
	}

	return str[:maxBytes-ellipsisLen] + "..."
}
