package relocator

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"infra.helper/pkg/app"
)

// Service is the long-lived state of the relocator subcommand.
type Service struct {
	cfg     Config
	store   *Store
	poster  *Poster
	metrics *Metrics
	buckets map[string]*bucketRuntime
}

// BucketSummary is the API view of a configured bucket.
type BucketSummary struct {
	Name         string `json:"name"`
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	Bucket       string `json:"bucket"`
	Prefix       string `json:"prefix"`
	DeleteAfter  bool   `json:"deleteAfter"`
	PollInterval string `json:"pollInterval"`
	InFlight     int    `json:"inFlight"`
}

// Start initialises the service, fans out per-bucket pollers and the HTTP
// status server, and blocks on the supplied lifecycle context.
func Start(ctx context.Context, cfg Config) error {
	mkdirErr := os.MkdirAll(cfg.WorkDir, tmpDirPerm)
	if mkdirErr != nil {
		return fmt.Errorf("mkdir work_dir: %w", mkdirErr)
	}

	store, storeErr := OpenStore(cfg.DBPath, cfg.EventLogSize)
	if storeErr != nil {
		return fmt.Errorf("open store: %w", storeErr)
	}

	svc := &Service{
		cfg:     cfg,
		store:   store,
		poster:  NewPoster(cfg.Post),
		metrics: NewMetrics(),
		buckets: make(map[string]*bucketRuntime, len(cfg.Buckets)),
	}

	for _, bcfg := range cfg.Buckets {
		runtime, rtErr := newBucketRuntime(bcfg)
		if rtErr != nil {
			log.Error().Err(rtErr).Str("bucket", bcfg.Name).Msg("init bucket failed")

			continue
		}

		svc.buckets[bcfg.Name] = runtime
	}

	for _, runtime := range svc.buckets {
		go svc.runPoller(ctx, runtime)
	}

	go svc.runHTTP(ctx)

	go svc.shutdownOnContext(ctx)

	app.SetReady(true)
	app.SetHealthy(true)

	return nil
}

// TriggerPoll wakes up the poller for the named bucket, if it exists.
func (s *Service) TriggerPoll(name string) bool {
	runtime, ok := s.buckets[name]
	if !ok {
		return false
	}

	select {
	case runtime.trigger <- struct{}{}:
	default:
	}

	return true
}

// BucketSummaries returns a sanitized view of every configured bucket.
func (s *Service) BucketSummaries() []BucketSummary {
	out := make([]BucketSummary, 0, len(s.buckets))

	for _, runtime := range s.buckets {
		out = append(out, BucketSummary{
			Name:         runtime.cfg.Name,
			Endpoint:     runtime.cfg.Endpoint,
			Region:       runtime.cfg.Region,
			Bucket:       runtime.cfg.Bucket,
			Prefix:       runtime.cfg.Prefix,
			DeleteAfter:  runtime.cfg.DeleteAfter,
			PollInterval: runtime.cfg.PollInterval.String(),
			InFlight:     int(runtime.busy.Load()),
		})
	}

	return out
}

func (s *Service) shutdownOnContext(ctx context.Context) {
	<-ctx.Done()

	closeErr := s.store.Close()
	if closeErr != nil {
		log.Error().Err(closeErr).Msg("close store")
	}
}

func (s *Service) recordEvent(evt Event) {
	appendErr := s.store.AppendEvent(evt)
	if appendErr != nil {
		log.Warn().Err(appendErr).Msg("append event")
	}
}

func newPollerJob(name string) (context.Context, func()) {
	return app.AddJob("relocator.poll." + name)
}
