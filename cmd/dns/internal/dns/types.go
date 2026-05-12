// Package dns contains the implementation of the dns subcommand.
//
//nolint:tagalign // mixed default/mapstructure/yaml tag widths look fine.
package dns

import "time"

// Config is the YAML config for the dns service.
//
//nolint:tagliatelle // snake_case yaml keys per TZ spec.
type Config struct {
	Server    ServerConfig    `mapstructure:"server"    yaml:"server"`
	Admin     AdminConfig     `mapstructure:"admin"     yaml:"admin"`
	Fakeip    FakeipConfig    `mapstructure:"fakeip"    yaml:"fakeip"`
	Upstreams []UpstreamEntry `mapstructure:"upstreams" yaml:"upstreams"`
	Rulesets  []RulesetEntry  `mapstructure:"rulesets"  yaml:"rulesets"`
	Cache     CacheConfig     `mapstructure:"cache"     yaml:"cache"`
	Log       LogConfig       `mapstructure:"log"       yaml:"log"`
	CacheDir  string          `default:"./cache"        mapstructure:"cache_dir" yaml:"cache_dir"`
	Proxy     string          `                         mapstructure:"proxy"     yaml:"proxy"`
}

// ServerConfig is the DNS listener config.
type ServerConfig struct {
	Listen    string   `default:"0.0.0.0:53"        mapstructure:"listen"    yaml:"listen"`
	Protocols []string `default:"[\"udp\",\"tcp\"]" mapstructure:"protocols" yaml:"protocols"`
}

// AdminConfig is the admin/metrics HTTP server config.
type AdminConfig struct {
	Listen string `default:":8080" mapstructure:"listen" yaml:"listen"`
}

// FakeipConfig holds the fake-IP generator settings.
//
//nolint:tagliatelle // snake_case yaml keys per TZ spec.
type FakeipConfig struct {
	IPv4Range string `default:"198.18.0.0/15"  mapstructure:"ipv4_range" yaml:"ipv4_range"`
	IPv6Range string `                         mapstructure:"ipv6_range" yaml:"ipv6_range"`
	TTL       uint32 `default:"1"              mapstructure:"ttl"        yaml:"ttl"`
}

// UpstreamEntry describes a single upstream resolver.
type UpstreamEntry struct {
	Address string        `             mapstructure:"address" yaml:"address"`
	Timeout time.Duration `default:"5s" mapstructure:"timeout" yaml:"timeout"`
}

// RulesetEntry is a single .dat source with action mapping per tag.
//
//nolint:tagliatelle // snake_case yaml keys per TZ spec.
type RulesetEntry struct {
	URL            string        `              mapstructure:"url"             yaml:"url"`
	UpdateInterval time.Duration `default:"24h" mapstructure:"update_interval" yaml:"update_interval"`
	Tags           []TagAction   `              mapstructure:"tags"            yaml:"tags"`
	// Proxy is the per-ruleset proxy URL override. Empty means use Config.Proxy.
	// Set to "direct" to bypass the global proxy for this ruleset.
	Proxy string `mapstructure:"proxy" yaml:"proxy"`
}

// ProxyDirect disables the global proxy for a single ruleset.
const ProxyDirect = "direct"

// TagAction maps a geosite tag to an action.
type TagAction struct {
	Name   string `mapstructure:"name"   yaml:"name"`
	Action string `mapstructure:"action" yaml:"action"`
}

// CacheConfig configures the upstream response cache.
//
//nolint:tagliatelle // snake_case yaml keys per TZ spec.
type CacheConfig struct {
	Enabled     bool `default:"true" mapstructure:"enabled"      yaml:"enabled"`
	Size        int  `default:"8192" mapstructure:"size"         yaml:"size"`
	TTLOverride bool `                mapstructure:"ttl_override" yaml:"ttl_override"`
}

// LogConfig configures logging level/format.
type LogConfig struct {
	Level  string `default:"info" mapstructure:"level"  yaml:"level"`
	Format string `default:"text" mapstructure:"format" yaml:"format"`
}

// Action constants.
const (
	ActionBlock  = "block"
	ActionFakeIP = "fakeip"
	ActionDirect = "direct"
)
