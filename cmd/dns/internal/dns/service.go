package dns

import (
	"fmt"

	"infra.helper/pkg/app"
)

// Start wires up the dns service and launches all background jobs.
//
// It returns once all listeners have been registered; the jobs themselves
// stay alive until the global app context is cancelled.
func Start(cfg Config) error {
	rules, err := newRulesetManager(cfg)
	if err != nil {
		return fmt.Errorf("init rulesets: %w", err)
	}

	pool, err := newFakeIPPool(cfg.Fakeip.IPv4Range, cfg.Fakeip.IPv6Range)
	if err != nil {
		return fmt.Errorf("init fakeip: %w", err)
	}

	upstream, err := newUpstreamPool(cfg.Upstreams)
	if err != nil {
		return fmt.Errorf("init upstreams: %w", err)
	}

	var cache *dnsCache
	if cfg.Cache.Enabled {
		cache = newDNSCache(cfg.Cache.Size)
	}

	metrics := newDNSMetrics()

	res := newResolver(cfg, rules, pool, cache, upstream, metrics)
	server := newDNSServer(cfg.Server, res)
	admin := newAdminServer(cfg.Admin, rules)

	go runRulesetManager(rules)
	go admin.Run()

	app.SetReady(true)
	app.SetHealthy(true)

	parentCtx, onStop := app.AddJob("dns.server")

	go func() {
		defer onStop()

		<-parentCtx.Done()
	}()

	runErr := server.Run(parentCtx)
	if runErr != nil {
		return fmt.Errorf("start dns server: %w", runErr)
	}

	return nil
}

func runRulesetManager(rules *rulesetManager) {
	ctx, onStop := app.AddJob("dns.rulesets")
	defer onStop()

	rules.Run(ctx)
}
