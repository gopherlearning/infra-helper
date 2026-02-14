package listupdater

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/rs/zerolog/log"
	"infra.helper/pkg/app"
)

const (
	originalDirName = "original"
	plainDirName    = "plain"

	clientTimeout         = 60 * time.Second
	readHeaderTimeout     = 10 * time.Second
	serverShutdownTimeout = 5 * time.Second
)

type service struct {
	cfg      config
	origDir  string
	plainDir string
	lists    map[string]list
	client   *http.Client
}

func startListUpdater(cfg config) {
	svc := &service{
		cfg:      cfg,
		origDir:  filepath.Join(cfg.Dir, originalDirName),
		plainDir: filepath.Join(cfg.Dir, plainDirName),
		lists:    make(map[string]list, len(cfg.Lists)),
		client:   &http.Client{Timeout: clientTimeout},
	}

	for _, l := range cfg.Lists {
		svc.lists[l.Name] = l
	}

	mkdirOrigErr := os.MkdirAll(svc.origDir, tmpDirPerm)
	if mkdirOrigErr != nil {
		log.Error().Err(mkdirOrigErr).Str("dir", svc.origDir).Msg("create original dir failed")

		return
	}

	mkdirPlainErr := os.MkdirAll(svc.plainDir, tmpDirPerm)
	if mkdirPlainErr != nil {
		log.Error().Err(mkdirPlainErr).Str("dir", svc.plainDir).Msg("create plain dir failed")

		return
	}

	go svc.runRefresher()
	go svc.runHTTP()
}

func (s *service) runRefresher() {
	jobCtx, onStop := app.AddJob("listUpdater.refresh")
	defer onStop()

	syncErr := s.syncAll(jobCtx)
	if syncErr != nil {
		log.Error().Err(syncErr).Msg("initial sync failed")
	}

	ticker := time.NewTicker(s.cfg.Refresh)
	defer ticker.Stop()

	for {
		select {
		case <-jobCtx.Done():
			return
		case <-ticker.C:
			syncErr := s.syncAll(jobCtx)
			if syncErr != nil {
				log.Error().Err(syncErr).Msg("sync failed")
			}
		}
	}
}

func (s *service) runHTTP() {
	jobCtx, onStop := app.AddJob("listUpdater.http")
	defer onStop()

	echoSrv := echo.New()
	echoSrv.Use(middleware.Recover())
	echoSrv.Use(app.EchoZerologMiddleware())

	echoSrv.GET("/healthz", func(ctx *echo.Context) error {
		return ctx.NoContent(http.StatusOK)
	})
	// Order matters: /plain/* must be registered before /:name.
	echoSrv.GET("/plain/:name", s.handlePlain)
	echoSrv.GET("/", s.handleIndex)
	echoSrv.GET("/:name", s.handleOriginal)

	startCfg := echo.StartConfig{
		Address:         s.cfg.Listen,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: serverShutdownTimeout,
		BeforeServeFunc: func(server *http.Server) error {
			server.ReadHeaderTimeout = readHeaderTimeout

			return nil
		},
	}

	log.Info().Str("addr", startCfg.Address).Msg("http server started")

	startErr := startCfg.Start(jobCtx, echoSrv)
	if startErr != nil {
		log.Error().Err(startErr).Msg("http server failed")
		app.Cancel()
	}
}

func (s *service) handleIndex(ctx *echo.Context) error {
	var body strings.Builder

	_, _ = body.WriteString("list-updater\n\n")
	for name := range s.lists {
		_, _ = fmt.Fprintf(&body, "/%s\n/plain/%s\n", name, name)
	}

	err := ctx.String(http.StatusOK, body.String())
	if err != nil {
		return fmt.Errorf("write index response: %w", err)
	}

	return nil
}

func (s *service) handleOriginal(ctx *echo.Context) error {
	const notFoundMsg = "not found"

	name := ctx.Param("name")
	if name == "" || strings.Contains(name, "/") {
		return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
	}

	if _, ok := s.lists[name]; !ok {
		return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
	}

	origPath := filepath.Join(s.origDir, name)

	err := ctx.File(origPath)
	if err != nil {
		return fmt.Errorf("serve original file: %w", err)
	}

	return nil
}

func (s *service) handlePlain(ctx *echo.Context) error {
	const notFoundMsg = "not found"

	name := ctx.Param("name")
	if name == "" || strings.Contains(name, "/") {
		return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
	}

	if _, ok := s.lists[name]; !ok {
		return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
	}

	origPath := filepath.Join(s.origDir, name)
	plainPath := filepath.Join(s.plainDir, name)

	_, origStatErr := os.Stat(origPath)
	if origStatErr != nil {
		if os.IsNotExist(origStatErr) {
			return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
		}

		log.Error().Err(origStatErr).Str("name", name).Msg("stat original failed")

		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}

	_, plainStatErr := os.Stat(plainPath)
	if plainStatErr != nil {
		genErr := ensurePlain(origPath, plainPath)
		if genErr != nil {
			log.Error().Err(genErr).Str("name", name).Msg("generate plain failed")

			return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
		}
	}

	ctx.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")

	err := ctx.File(plainPath)
	if err != nil {
		return fmt.Errorf("serve plain file: %w", err)
	}

	return nil
}
