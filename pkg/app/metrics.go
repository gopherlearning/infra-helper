package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

func startMetrics(ctx context.Context, onStop func()) {
	defer onStop()

	isHealthy.Store(false)
	isReady.Store(false)

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			switch req.URL.Path {
			// case "/targets":
			// 	t := fack.GetTargets()
			// 	keys := make([]string, 0)

			// 	for k := range t {
			// 		keys = append(keys, k)
			// 	}

			// 	sort.Strings(keys)

			// 	for _, k := range keys {
			// 		resp.Write([]byte(k + ":\n"))

			// 		for _, v := range t[k] {
			// 			resp.Write([]byte(fmt.Sprintf(`  - "%s"%s`, v, "\n")))
			// 		}
			// 	}

			// 	resp.WriteHeader(http.StatusOK)

			// 	return
			case "/readyz":
				if r, ok := isReady.Load().(bool); !r || !ok {
					resp.WriteHeader(http.StatusServiceUnavailable)

					return
				}

				resp.WriteHeader(http.StatusOK)

				return
			case "/healthz":
				if r, ok := isHealthy.Load().(bool); !r || !ok {
					resp.WriteHeader(http.StatusServiceUnavailable)

					return
				}

				resp.WriteHeader(http.StatusOK)

				return
			}
		}

		promhttp.HandlerFor(metrics, promhttp.HandlerOpts{}).ServeHTTP(resp, req)
	})

	const (
		readHeaderTimeout    = 60 * time.Second
		shutdownGraceTimeout = 2 * time.Second
	)

	server := &http.Server{
		Addr:              metricPort(),
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGraceTimeout)
		defer cancel()

		shutdownErr := server.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			log.Error().Err(shutdownErr).Msg("shutdown metrics server")
		}
	}()

	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error().Err(err).Msg("start metrics failed")

		return
	}
}

// func (d StartMetrics) AfterApplyerror {
// 	if d {
// 		StartMetrics(AddJob("metrics"), wg)
// 	}

// 	return nil
// }

// SetReady sets the readiness probe state.
func SetReady(r bool) {
	isReady.Store(r)
}

// SetHealthy sets the liveness probe state.
func SetHealthy(h bool) {
	isHealthy.Store(h)
}

func metricPort() string {
	port := os.Getenv("METRICS")
	if len(port) != 0 && strings.ContainsRune(port, ':') {
		return port
	}

	return ":9100"
}

// StartMetrics configures and starts the metrics HTTP server.
//
// Call it in a goroutine if you want it to be non-blocking.
func StartMetrics(ctx context.Context, onStop func()) {
	appMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "app",
		Help: "Application name",
		ConstLabels: prometheus.Labels{
			"name": name,
		}},
	)

	appMetric.Set(1)

	metrics.MustRegister(
		appMetric,
		jobMetric,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),
	)

	startMetrics(ctx, onStop)
}

// func enableCollectors(nc *collector.NodeCollector, collectors ...string) {
// 	for _, c := range collectors {
// 		if _, ok := nc.Collectors[c]; !ok {
// 			// Включаем коллектор, если он доступен
// 			if collectorConstructor, found := collector.[c]; found {
// 				enabledCollector, err := collectorConstructor()
// 				if err != nil {
// 					log.Error().Err(err).Msgf("failed to create collector %s", c)
// 					continue
// 				}
// 				nc.Collectors[c] = enabledCollector
// 			} else {
// 				log.Warn().Msgf("collector %s not found", c)
// 			}
// 		}
// 	}
// }
