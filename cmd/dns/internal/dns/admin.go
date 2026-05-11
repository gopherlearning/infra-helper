package dns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"infra.helper/pkg/app"
)

const (
	adminReadHeaderTimeout = 10 * time.Second
	adminShutdownTimeout   = 2 * time.Second
)

// adminServer exposes /healthz, /metrics, and POST /reload on a separate port.
type adminServer struct {
	cfg   AdminConfig
	rules *rulesetManager
}

func newAdminServer(cfg AdminConfig, rules *rulesetManager) *adminServer {
	return &adminServer{cfg: cfg, rules: rules}
}

// Run starts the admin HTTP server. It blocks until the parent context cancels.
func (a *adminServer) Run() {
	ctx, onStop := app.AddJob("dns.admin")
	defer onStop()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealthz)
	mux.Handle("/metrics", promhttp.HandlerFor(app.Metrics(), promhttp.HandlerOpts{}))
	mux.HandleFunc("/reload", a.handleReload)

	server := &http.Server{
		Addr:              a.cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: adminReadHeaderTimeout,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), adminShutdownTimeout)
		defer cancel()

		err := server.Shutdown(shutdownCtx)
		if err != nil {
			log.Error().Err(err).Msg("dns admin shutdown failed")
		}
	}()

	log.Info().Str("addr", a.cfg.Listen).Msg("dns admin server started")

	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error().Err(err).Msg("dns admin listener exited")
		app.Cancel()
	}
}

func (a *adminServer) handleHealthz(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(writer, "ok")
}

func (a *adminServer) handleReload(writer http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writer.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	a.rules.TriggerReload()
	writer.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintln(writer, "reload scheduled")
}
