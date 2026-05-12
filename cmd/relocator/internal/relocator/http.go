package relocator

import (
	"context"
	"crypto/subtle"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/rs/zerolog/log"
	"infra.helper/pkg/app"
)

//go:embed web
var webFS embed.FS

const (
	httpReadHeaderTimeout = 10 * time.Second
	httpGracefulTimeout   = 5 * time.Second
)

func (s *Service) runHTTP(_ context.Context) {
	jobCtx, onStop := app.AddJob("relocator.http")
	defer onStop()

	echoSrv := echo.New()
	echoSrv.Use(middleware.Recover())
	echoSrv.Use(app.EchoZerologMiddleware())

	if s.cfg.BasicAuth.User != "" {
		echoSrv.Use(basicAuthMiddleware(s.cfg.BasicAuth))
	}

	staticSub, subErr := fs.Sub(webFS, "web")
	if subErr != nil {
		log.Error().Err(subErr).Msg("embed static fs")

		return
	}

	echoSrv.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", http.FileServer(http.FS(staticSub)))))

	echoSrv.GET("/", s.handleIndex(staticSub))
	echoSrv.GET("/healthz", func(ectx *echo.Context) error { return ectx.NoContent(http.StatusOK) })
	echoSrv.GET("/api/state", s.handleState)
	echoSrv.GET("/api/events", s.handleEvents)
	echoSrv.GET("/api/buckets", s.handleBuckets)
	echoSrv.POST("/api/buckets/:name/poll", s.handlePollTrigger)

	startCfg := echo.StartConfig{
		Address:         s.cfg.Listen,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: httpGracefulTimeout,
		BeforeServeFunc: func(server *http.Server) error {
			server.ReadHeaderTimeout = httpReadHeaderTimeout

			return nil
		},
	}

	log.Info().Str("addr", startCfg.Address).Msg("relocator status server started")

	startErr := startCfg.Start(jobCtx, echoSrv) //nolint:contextcheck // jobCtx descends from the global app context
	if startErr != nil {
		log.Error().Err(startErr).Msg("relocator http server failed")
		app.Cancel()
	}
}

func (s *Service) handleIndex(static fs.FS) echo.HandlerFunc {
	return func(ectx *echo.Context) error {
		body, readErr := fs.ReadFile(static, "index.html")
		if readErr != nil {
			return fmt.Errorf("read index: %w", readErr)
		}

		blobErr := ectx.HTMLBlob(http.StatusOK, body)
		if blobErr != nil {
			return fmt.Errorf("write index: %w", blobErr)
		}

		return nil
	}
}

type stateResponse struct {
	Totals  map[string]uint64 `json:"totals"`
	Buckets []bucketState     `json:"buckets"`
}

type bucketState struct {
	Config BucketSummary `json:"config"`
	Stats  BucketStats   `json:"stats"`
}

func (s *Service) handleState(ectx *echo.Context) error {
	stats, statsErr := s.store.AllStats()
	if statsErr != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, statsErr.Error())
	}

	summaries := s.BucketSummaries()
	byName := make(map[string]BucketSummary, len(summaries))

	for _, sum := range summaries {
		byName[sum.Name] = sum
	}

	statsByName := make(map[string]BucketStats, len(stats))
	for _, st := range stats {
		statsByName[st.Name] = st
	}

	resp := stateResponse{
		Totals:  map[string]uint64{},
		Buckets: make([]bucketState, 0, len(summaries)),
	}

	for _, sum := range summaries {
		st := statsByName[sum.Name]
		st.Name = sum.Name
		resp.Buckets = append(resp.Buckets, bucketState{Config: sum, Stats: st})
		addTotals(resp.Totals, st)
	}

	jsonErr := ectx.JSON(http.StatusOK, resp)
	if jsonErr != nil {
		return fmt.Errorf("encode state: %w", jsonErr)
	}

	return nil
}

func addTotals(totals map[string]uint64, stats BucketStats) {
	totals["downloaded"] += stats.Downloaded
	totals["extracted"] += stats.Extracted
	totals["posted"] += stats.Posted
	totals["deleted"] += stats.Deleted
	totals["skipped"] += stats.Skipped
	totals["downloadFailed"] += stats.DownloadFailed
	totals["extractFailed"] += stats.ExtractFailed
	totals["passwordFailed"] += stats.PasswordFailed
	totals["postFailed"] += stats.PostFailed
}

func (s *Service) handleEvents(ectx *echo.Context) error {
	limit := 100

	raw := ectx.QueryParam("limit")
	if raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}

	events, eventsErr := s.store.RecentEvents(limit)
	if eventsErr != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, eventsErr.Error())
	}

	jsonErr := ectx.JSON(http.StatusOK, events)
	if jsonErr != nil {
		return fmt.Errorf("encode events: %w", jsonErr)
	}

	return nil
}

func (s *Service) handleBuckets(ectx *echo.Context) error {
	jsonErr := ectx.JSON(http.StatusOK, s.BucketSummaries())
	if jsonErr != nil {
		return fmt.Errorf("encode buckets: %w", jsonErr)
	}

	return nil
}

func (s *Service) handlePollTrigger(ectx *echo.Context) error {
	name := ectx.Param("name")
	if !s.TriggerPoll(name) {
		return echo.NewHTTPError(http.StatusNotFound, "bucket not found")
	}

	jsonErr := ectx.JSON(http.StatusAccepted, map[string]string{"status": "queued", "bucket": name})
	if jsonErr != nil {
		return fmt.Errorf("encode trigger: %w", jsonErr)
	}

	return nil
}

func basicAuthMiddleware(creds BasicAuth) echo.MiddlewareFunc {
	expectedUser := []byte(creds.User)
	expectedPass := []byte(creds.Pass)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ectx *echo.Context) error {
			user, pass, ok := ectx.Request().BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(user), expectedUser) != 1 ||
				subtle.ConstantTimeCompare([]byte(pass), expectedPass) != 1 {
				ectx.Response().Header().Set("WWW-Authenticate", `Basic realm="relocator"`)

				return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
			}

			return next(ectx)
		}
	}
}
