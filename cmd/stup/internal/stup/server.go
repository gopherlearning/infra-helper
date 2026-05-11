// Package stup serves a static stub sign-in page.
package stup

import (
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/rs/zerolog/log"
	"infra.helper/pkg/app"
)

//go:embed index.html
var indexHTML []byte

const (
	readHeaderTimeout     = 10 * time.Second
	serverShutdownTimeout = 5 * time.Second
)

// Start launches the static stub HTTP server in a background job.
func Start(listen string) {
	go runHTTP(listen)
}

func runHTTP(listen string) {
	jobCtx, onStop := app.AddJob("stup.http")
	defer onStop()

	echoSrv := echo.New()
	echoSrv.Use(middleware.Recover())
	echoSrv.Use(app.EchoZerologMiddleware())

	echoSrv.GET("/healthz", func(ctx *echo.Context) error {
		return ctx.NoContent(http.StatusOK)
	})

	echoSrv.GET("/", handleIndex)

	startCfg := echo.StartConfig{
		Address:         listen,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: serverShutdownTimeout,
		BeforeServeFunc: func(server *http.Server) error {
			server.ReadHeaderTimeout = readHeaderTimeout

			return nil
		},
	}

	log.Info().Str("addr", listen).Msg("stup http server started")

	startErr := startCfg.Start(jobCtx, echoSrv)
	if startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
		log.Error().Err(startErr).Msg("stup http server failed")
		app.Cancel()
	}
}

func handleIndex(ctx *echo.Context) error {
	err := ctx.HTMLBlob(http.StatusOK, indexHTML)
	if err != nil {
		return fmt.Errorf("write index response: %w", err)
	}

	return nil
}
