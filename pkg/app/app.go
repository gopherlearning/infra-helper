// Package app provides shared runtime primitives for infra-helper subcommands.
//
// It is responsible for process lifecycle (signals + cancellation), logging,
// metrics wiring, and background job tracking via AddJob.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creasty/defaults"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

var (
	isHealthy = new(atomic.Value)
	isReady   = new(atomic.Value)
	metrics   *prometheus.Registry
	waitGroup = &sync.WaitGroup{}

	// Jobs is the global set of registered background job names.
	Jobs      = &sync.Map{}
	jobMetric prometheus.GaugeVec
	globalctx context.Context
	cancelCtx context.CancelFunc
	name      string
)

// Metrics returns the Prometheus registry used by the app.
func Metrics() *prometheus.Registry {
	return metrics
}

// RegisterMetric registers collectors in the global registry.
func RegisterMetric(cs ...prometheus.Collector) {
	for _, c := range cs {
		err := metrics.Register(c)
		if err != nil && !strings.Contains(err.Error(), "duplicate") {
			log.Error().Err(err).Msg("")
		}
	}
}

// WG exposes the global background job wait group.
func WG() *sync.WaitGroup {
	return waitGroup
}

// Init configures logging, installs signal handling, and starts graceful shutdown.
func Init(verbose bool) {
	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)

		log.Logger = log.Logger.
			Level(zerolog.DebugLevel).
			Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "2006-01-02 15:04:05.99"}).
			With().Caller().Logger()
	} else {
		zerolog.TimeFieldFormat = "2006-01-02 15:04:05.99"
		log.Logger = log.Logger.
			Level(zerolog.InfoLevel).With().Logger()
	}

	globalctx = context.Background()
	globalctx, cancelCtx = context.WithCancel(globalctx)
	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		const shutdownTimeout = 10 * time.Second

		waitDone := make(chan struct{})

		go func() {
			waitGroup.Wait()
			close(waitDone)
		}()

		select {
		case <-terminate:
			log.Info().Msg("stopped by terminate signal")
			cancelCtx()
		case <-globalctx.Done():
			log.Info().Msg("stopped by Cancel call")
		}

		select {
		case <-waitDone:
			log.Info().Msg("all jobs stopped")
			os.Exit(0)
		case <-time.NewTimer(shutdownTimeout).C:
			log.Error().Msg("Program exit by timeout")
			os.Exit(1)
		}
	}()
}

// Cancel requests a graceful shutdown.
func Cancel() {
	cancelCtx()
}

// AddJob registers a background job in the global lifecycle.
//
// It returns a context tied to the app lifecycle and an onStop callback.
// The caller must `defer onStop()` exactly once.
func AddJob(name string) (context.Context, func()) {
	if len(name) == 0 {
		log.Fatal().Msgf("null job name: %s", name)
	}

	_, loaded := Jobs.LoadOrStore(name, struct{}{})
	if loaded {
		log.Fatal().Msgf("duplicate job name: %s", name)
	}

	waitGroup.Add(1)

	if jobMetric.MetricVec != nil {
		jobMetric.With(prometheus.Labels{"name": name}).Set(1)
	}

	log.Info().Msgf("%s started", name)

	return context.WithValue(globalctx, ContextKeyJobName{}, name), func() {
		log.Info().Msgf("%s stopped", name)

		if jobMetric.MetricVec != nil {
			jobMetric.Delete(prometheus.Labels{"name": name})
		}

		Jobs.Delete(name)

		waitGroup.Done()
	}
}

// // StartStopFunc .
// func StartStopFunc() func() {

// }

// ReadFromFile reads YAML config into out, creating a default file if missing.
func ReadFromFile(filename string, out any) error {
	const configFilePerm os.FileMode = 0o600

	if len(filename) == 0 {
		return nil
	}

	_, statErr := os.Stat(filename)
	if errors.Is(statErr, os.ErrNotExist) {
		yamlData, marshalErr := yaml.Marshal(out)
		if marshalErr != nil {
			return fmt.Errorf("marshal default config: %w", marshalErr)
		}

		writeErr := os.WriteFile(filename, yamlData, configFilePerm)
		if writeErr != nil {
			return fmt.Errorf("write default config file: %w", writeErr)
		}
	}

	setErr := defaults.Set(out)
	if setErr != nil {
		return fmt.Errorf("apply defaults: %w", setErr)
	}

	viper.AutomaticEnv()
	viper.SetConfigFile(filename)

	readErr := viper.ReadInConfig()
	if readErr != nil {
		return fmt.Errorf("read config: %w", readErr)
	}

	unmarshalErr := viper.Unmarshal(out)
	if unmarshalErr != nil {
		log.Error().Err(unmarshalErr).Msg("unmarshal config")

		return fmt.Errorf("unmarshal config: %w", unmarshalErr)
	}

	return nil
}

// ContextKeyJobName is a context key used to store the job name.
type ContextKeyJobName struct{}
