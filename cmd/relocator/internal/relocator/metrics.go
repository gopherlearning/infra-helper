package relocator

import (
	"github.com/prometheus/client_golang/prometheus"
	"infra.helper/pkg/app"
)

// Metrics groups all Prometheus collectors emitted by the subcommand.
type Metrics struct {
	Objects      *prometheus.CounterVec
	Bytes        *prometheus.CounterVec
	Files        *prometheus.CounterVec
	PollDuration *prometheus.HistogramVec
	InFlight     *prometheus.GaugeVec
}

// NewMetrics constructs and registers the relocator's Prometheus collectors.
func NewMetrics() *Metrics {
	metrics := &Metrics{
		Objects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relocator_objects_total",
			Help: "Number of S3 objects processed, partitioned by bucket and outcome.",
		}, []string{"bucket", "status"}),
		Bytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relocator_bytes_total",
			Help: "Total bytes downloaded from each source bucket.",
		}, []string{"bucket"}),
		Files: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relocator_files_total",
			Help: "JSON files extracted and POSTed, partitioned by bucket and outcome.",
		}, []string{"bucket", "status"}),
		PollDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "relocator_poll_duration_seconds",
			Help:    "Wall-clock duration of one bucket poll cycle.",
			Buckets: prometheus.DefBuckets,
		}, []string{"bucket"}),
		InFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "relocator_inflight",
			Help: "Number of objects currently being processed per bucket.",
		}, []string{"bucket"}),
	}

	app.RegisterMetric(metrics.Objects, metrics.Bytes, metrics.Files, metrics.PollDuration, metrics.InFlight)

	return metrics
}
