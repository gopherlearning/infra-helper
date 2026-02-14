package listupdater

import (
	"errors"
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
	plainCatsDir    = "categories"

	clientTimeout         = 60 * time.Second
	readHeaderTimeout     = 10 * time.Second
	serverShutdownTimeout = 5 * time.Second
)

type service struct {
	cfg      Config
	origDir  string
	plainDir string
	lists    map[string]List
	client   *http.Client
}

func startListUpdater(cfg Config) {
	svc := &service{
		cfg:      cfg,
		origDir:  filepath.Join(cfg.Dir, originalDirName),
		plainDir: filepath.Join(cfg.Dir, plainDirName),
		lists:    make(map[string]List, len(cfg.Lists)),
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

// compile-time dependency: shared perms from sync.go

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
	echoSrv.GET("/plain/:name/", s.handlePlainCategories)
	echoSrv.GET("/plain/:name/:category", s.handlePlainCategory)
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
	if !isSafeSegment(name) {
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
	if !isSafeSegment(name) {
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

func (s *service) handlePlainCategory(ctx *echo.Context) error {
	const notFoundMsg = "not found"

	name := ctx.Param("name")

	category := ctx.Param("category")
	if !isSafeSegment(name) || !isSafeSegment(category) {
		return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
	}

	if _, ok := s.lists[name]; !ok {
		return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
	}

	origPath := filepath.Join(s.origDir, name)
	plainPath := filepath.Join(s.plainDir, plainCatsDir, name, category)

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
		genErr := ensurePlainCategory(origPath, plainPath, category)

		httpErr := mapCategoryPlainError(genErr, name, category)
		if httpErr != nil {
			return httpErr
		}
	}

	ctx.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")

	err := ctx.File(plainPath)
	if err != nil {
		return fmt.Errorf("serve category plain file: %w", err)
	}

	return nil
}

func (s *service) handlePlainCategories(ctx *echo.Context) error {
	const notFoundMsg = "not found"

	name := ctx.Param("name")
	if !isSafeSegment(name) {
		return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
	}

	if _, ok := s.lists[name]; !ok {
		return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
	}

	origPath := filepath.Join(s.origDir, name)

	_, origStatErr := os.Stat(origPath)
	if origStatErr != nil {
		if os.IsNotExist(origStatErr) {
			return echo.NewHTTPError(http.StatusNotFound, notFoundMsg)
		}

		log.Error().Err(origStatErr).Str("name", name).Msg("stat original failed")

		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}

	categories, isDat, catErr := listGeoSiteCategoriesFromFile(origPath)
	if catErr != nil {
		log.Error().Err(catErr).Str("name", name).Msg("list categories failed")

		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}

	if !isDat {
		return echo.NewHTTPError(http.StatusBadRequest, "categories are supported only for .dat sources")
	}

	ctx.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")

	err := ctx.String(http.StatusOK, strings.Join(categories, "\n")+"\n")
	if err != nil {
		return fmt.Errorf("write categories response: %w", err)
	}

	return nil
}

func mapCategoryPlainError(err error, name string, category string) *echo.HTTPError {
	if err == nil {
		return nil
	}

	if errors.Is(err, errCategoryNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "not found")
	}

	if errors.Is(err, errCategoryUnsupported) {
		return echo.NewHTTPError(http.StatusBadRequest, "categories are supported only for .dat sources")
	}

	log.Error().Err(err).Str("name", name).Str("category", category).Msg("generate category plain failed")

	return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
}

func isSafeSegment(seg string) bool {
	if seg == "" {
		return false
	}

	if strings.Contains(seg, "/") || strings.Contains(seg, "\\") {
		return false
	}

	if strings.Contains(seg, "..") {
		return false
	}

	for i := range len(seg) {
		b := seg[i]
		switch {
		case b >= 'a' && b <= 'z':
		case b >= 'A' && b <= 'Z':
		case b >= '0' && b <= '9':
		case b == '.', b == '-', b == '_':
		default:
			return false
		}
	}

	return true
}
