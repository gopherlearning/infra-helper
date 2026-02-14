package app

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// StartMetrics .
//
//nolint:contextcheck
func startMetrics() {
	ctx, onStop := AddJob("metrics")

	defer onStop()

	isHealthy.Store(false)
	isReady.Store(false)

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
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

	h := &http.Server{
		Addr:              metricPort(),
		Handler:           handler,
		ReadHeaderTimeout: 60 * time.Second,
	}
	ctxDead, cancel := context.WithTimeout(context.Background(), 2*time.Second)

	go func() {
		<-ctx.Done()

		defer cancel()

		if err := h.Shutdown(ctxDead); err != nil {
			log.Err(err)
		}
	}()

	err := h.ListenAndServe()
	if err != nil {
		if err.Error() != "http: Server closed" {
			log.Error().Err(err).Msg("start metrics failed")
			return
		}
	}

	cancel()
}

// func (d StartMetrics) AfterApplyerror {
// 	if d {
// 		StartMetrics(AddJob("metrics"), wg)
// 	}

// 	return nil
// }

func SetReady(r bool) {
	isReady.Store(r)
}

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

func StartMetrics(ctx context.Context, wg *sync.WaitGroup) {
	appMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "app",
		Help: "Application name",
		ConstLabels: prometheus.Labels{
			"name": name,
		}},
	)

	appMetric.Set(1)

	jobMetric = *prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "job",
		Help: "Job name",
	}, []string{"name"},
	)

	metrics.MustRegister(appMetric, jobMetric, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), collectors.NewBuildInfoCollector())

	go startMetrics()
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
