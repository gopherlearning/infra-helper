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
	isHealthy   = new(atomic.Value)
	isReady     = new(atomic.Value)
	metrics     *prometheus.Registry
	wg          = &sync.WaitGroup{}
	Jobs        = &sync.Map{}
	jobMetric   prometheus.GaugeVec
	globalctx   context.Context
	cancelCtx   context.CancelFunc
	name        string
	description string
)

func Metrics() *prometheus.Registry {
	return metrics
}

// RegisterMetric .
func RegisterMetric(cs ...prometheus.Collector) {
	for _, c := range cs {
		err := metrics.Register(c)
		if err != nil && !strings.Contains(err.Error(), "duplicate") {
			log.Error().Err(err).Msg("")
		}
	}
}

// WG .
func WG() *sync.WaitGroup {
	return wg
}

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

	globalctx, cancelCtx = context.WithCancel(context.Background())
	terminate := make(chan os.Signal, 1)
	wg := &sync.WaitGroup{}

	signal.Notify(terminate, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	wg.Add(1)

	go func() {
		waitDone := make(chan struct{})
		go func() {
			wg.Wait()
			waitDone <- struct{}{}
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
		case <-time.NewTimer(10 * time.Second).C:
			log.Error().Msg("Program exit by timeout")
			os.Exit(1)
		}

		os.Exit(0)
	}()
}

func Cancel() {
	cancelCtx()
}

// AddJob .
func AddJob(name string) (context.Context, func()) {
	if len(name) == 0 {
		panic(fmt.Sprintf("null job name: %s", name))
	}

	wg.Add(1)

	_, ok := Jobs.Load(name)
	if ok {
		panic(fmt.Sprintf("dublicate job name: %s", name))
	}

	if jobMetric.MetricVec != nil {
		jobMetric.With(prometheus.Labels{"name": name}).Set(1)
	}

	Jobs.Store(name, struct{}{})

	log.Info().Msgf("%s started", name)

	return context.WithValue(globalctx, ContextKeyJobName{}, name), func() {
		log.Info().Msgf("%s stopped", name)

		if jobMetric.MetricVec != nil {
			jobMetric.Delete(prometheus.Labels{"name": name})
		}

		Jobs.Delete(name)

		wg.Done()
	}
}

// // StartStopFunc .
// func StartStopFunc() func() {

// }

func ReadFromFile(filename string, out interface{}) error {
	if len(filename) == 0 {
		return nil
	}

	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		yamlData, err := yaml.Marshal(out)
		if err != nil {
			return err
		}

		err = os.WriteFile(filename, yamlData, 0600)
		if err != nil {
			return err
		}
	}

	if err := defaults.Set(out); err != nil {
		return err
	}

	viper.AutomaticEnv()
	viper.SetConfigFile(filename)

	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	if err := viper.Unmarshal(out); err != nil {
		log.Error().Err(err).Msg("")
		return err
	}

	return nil
}

type ContextKeyJobName struct{}
