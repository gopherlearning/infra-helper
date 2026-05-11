package dns

import (
	"github.com/prometheus/client_golang/prometheus"
	"infra.helper/pkg/app"
)

// dnsMetrics groups Prometheus collectors for the dns subcommand.
type dnsMetrics struct {
	queries        *prometheus.CounterVec
	fakeipHits     prometheus.Counter
	blockHits      prometheus.Counter
	cacheHits      prometheus.Counter
	cacheMisses    prometheus.Counter
	upstreamErrors *prometheus.CounterVec
	upstreamLat    *prometheus.HistogramVec
}

func newDNSMetrics() *dnsMetrics {
	collectors := &dnsMetrics{
		queries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dns",
			Name:      "queries_total",
			Help:      "Total DNS queries received, labelled by qtype and outcome.",
		}, []string{"qtype", "outcome"}),
		fakeipHits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "dns",
			Name:      "fakeip_hits_total",
			Help:      "Number of queries answered with a fakeip.",
		}),
		blockHits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "dns",
			Name:      "block_hits_total",
			Help:      "Number of queries blocked (NXDOMAIN).",
		}),
		cacheHits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "dns",
			Name:      "cache_hits_total",
			Help:      "Cache hits.",
		}),
		cacheMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "dns",
			Name:      "cache_misses_total",
			Help:      "Cache misses.",
		}),
		upstreamErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dns",
			Name:      "upstream_errors_total",
			Help:      "Errors when querying upstream resolvers.",
		}, []string{"address"}),
		upstreamLat: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "dns",
			Name:      "upstream_latency_seconds",
			Help:      "Upstream resolver latency.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"address"}),
	}

	app.RegisterMetric(
		collectors.queries,
		collectors.fakeipHits,
		collectors.blockHits,
		collectors.cacheHits,
		collectors.cacheMisses,
		collectors.upstreamErrors,
		collectors.upstreamLat,
	)

	return collectors
}
