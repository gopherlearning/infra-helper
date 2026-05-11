// Package dns implements the "dns" Cobra subcommand.
//
// It runs a recursive DNS server that returns deterministic fake IPs for
// domains matched by configured v2ray geosite rulesets and proxies the rest
// to upstream resolvers (UDP/TCP/DoT/DoH).
package dns

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	svc "infra.helper/cmd/dns/internal/dns"
	"infra.helper/pkg/app"
)

var (
	configPath string

	errInvalidConfig    = errors.New("invalid config")
	errListenRequired   = errors.New("server.listen is required")
	errProtocolRequired = errors.New("server.protocols must contain at least one of udp/tcp")
	errFakeipRange      = errors.New("fakeip.ipv4_range is required")
	errUpstreamRequired = errors.New("at least one upstream is required")
)

var dnsCmd = &cobra.Command{
	Use:   "dns",
	Short: "Recursive DNS server with fakeip for blocked domains",
	Run: func(_ *cobra.Command, _ []string) {
		ctx, onStop := app.AddJob("dns")
		defer onStop()

		var cfg svc.Config

		readErr := app.ReadFromFile(configPath, &cfg)
		if readErr != nil {
			log.Error().Err(readErr).Msg("config broken")

			return
		}

		validateErr := validateConfig(cfg)
		if validateErr != nil {
			log.Error().Err(validateErr).Msg("invalid config")

			return
		}

		log.Info().
			Str("listen", cfg.Server.Listen).
			Strs("protocols", cfg.Server.Protocols).
			Str("admin", cfg.Admin.Listen).
			Int("upstreams", len(cfg.Upstreams)).
			Int("rulesets", len(cfg.Rulesets)).
			Msg("dns started")

		startErr := svc.Start(cfg)
		if startErr != nil {
			log.Error().Err(startErr).Msg("dns start failed")
			app.Cancel()

			return
		}

		<-ctx.Done()
	},
}

// Register registers the dns command on the parent.
func Register(parent *cobra.Command) {
	parent.AddCommand(dnsCmd)
}

func init() {
	dnsCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "configuration file path")
}

func validateConfig(cfg svc.Config) error {
	serverErr := validateServer(cfg.Server)
	if serverErr != nil {
		return serverErr
	}

	if cfg.Fakeip.IPv4Range == "" {
		return errFakeipRange
	}

	upstreamErr := validateUpstreams(cfg.Upstreams)
	if upstreamErr != nil {
		return upstreamErr
	}

	return validateRulesets(cfg.Rulesets)
}

func validateServer(srv svc.ServerConfig) error {
	if srv.Listen == "" {
		return errListenRequired
	}

	if len(srv.Protocols) == 0 {
		return errProtocolRequired
	}

	for _, proto := range srv.Protocols {
		if proto != "udp" && proto != "tcp" {
			return fmt.Errorf("%w: unknown protocol %q", errInvalidConfig, proto)
		}
	}

	return nil
}

func validateUpstreams(ups []svc.UpstreamEntry) error {
	if len(ups) == 0 {
		return errUpstreamRequired
	}

	for idx, up := range ups {
		if up.Address == "" {
			return fmt.Errorf("%w: upstreams[%d].address is required", errInvalidConfig, idx)
		}
	}

	return nil
}

func validateRulesets(rulesets []svc.RulesetEntry) error {
	seenURL := make(map[string]struct{}, len(rulesets))

	for idx, ruleset := range rulesets {
		if ruleset.URL == "" {
			return fmt.Errorf("%w: rulesets[%d].url is required", errInvalidConfig, idx)
		}

		if _, ok := seenURL[ruleset.URL]; ok {
			return fmt.Errorf("%w: duplicate ruleset url: %s", errInvalidConfig, ruleset.URL)
		}

		seenURL[ruleset.URL] = struct{}{}

		tagsErr := validateTags(ruleset.Tags, idx)
		if tagsErr != nil {
			return tagsErr
		}
	}

	return nil
}

func validateTags(tags []svc.TagAction, rulesetIdx int) error {
	for tagIdx, tag := range tags {
		if tag.Name == "" {
			return fmt.Errorf("%w: rulesets[%d].tags[%d].name is required", errInvalidConfig, rulesetIdx, tagIdx)
		}

		switch tag.Action {
		case "fakeip", "block", "direct":
		default:
			return fmt.Errorf("%w: rulesets[%d].tags[%d].action must be fakeip|block|direct",
				errInvalidConfig, rulesetIdx, tagIdx)
		}
	}

	return nil
}
