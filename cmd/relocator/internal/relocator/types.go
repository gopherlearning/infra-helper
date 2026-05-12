// Package relocator implements the "relocator" Cobra subcommand.
//
// It periodically polls S3-compatible buckets across providers, downloads new
// archives, optionally extracts them (with a password list), POSTs every JSON
// entry inside the archive to a configured URL, and tracks per-bucket
// statistics in an embedded BoltDB. The current state is rendered through an
// embedded HTTP status page served by the Echo router.
package relocator

import "time"

// Config is the YAML config for the relocator service.
//
//nolint:tagliatelle // snake_case yaml keys per project convention.
type Config struct {
	Listen        string        `default:":8090"                 mapstructure:"listen"        yaml:"listen"`
	DBPath        string        `default:"./data/relocator.db"   mapstructure:"db_path"       yaml:"db_path"`
	WorkDir       string        `default:"./data/relocator/work" mapstructure:"work_dir"      yaml:"work_dir"`
	QuarantineDir string        `default:"./data/relocator/quar" mapstructure:"quarantine"    yaml:"quarantine"`
	PollInterval  time.Duration `default:"1m"                    mapstructure:"poll_interval" yaml:"poll_interval"`
	MaxWorkers    int           `default:"4"                     mapstructure:"max_workers"   yaml:"max_workers"`
	EventLogSize  int           `default:"500"                   mapstructure:"event_log"     yaml:"event_log"`
	BasicAuth     BasicAuth     `mapstructure:"basic_auth"       yaml:"basic_auth"`

	Post      PostConfig `mapstructure:"post"      yaml:"post"`
	Passwords []string   `mapstructure:"passwords" yaml:"passwords"`
	Buckets   []Bucket   `mapstructure:"buckets"   yaml:"buckets"`
}

// BasicAuth optionally protects the status page.
type BasicAuth struct {
	User string `mapstructure:"user" yaml:"user"`
	Pass string `mapstructure:"pass" yaml:"pass"`
}

// PostConfig describes the HTTP POST target for extracted JSON files.
//
//nolint:tagliatelle // snake_case yaml keys per project convention.
type PostConfig struct {
	URL          string            `mapstructure:"url"         yaml:"url"`
	Timeout      time.Duration     `default:"30s"              mapstructure:"timeout"       yaml:"timeout"`
	Retries      int               `default:"3"                mapstructure:"retries"       yaml:"retries"`
	RetryBackoff time.Duration     `default:"5s"               mapstructure:"retry_backoff" yaml:"retry_backoff"`
	Headers      map[string]string `mapstructure:"headers"     yaml:"headers"`
	HMACSecret   string            `mapstructure:"hmac_secret" yaml:"hmac_secret"`
	HMACHeader   string            `default:"X-Signature"      mapstructure:"hmac_header"   yaml:"hmac_header"`
}

// Bucket describes one S3-compatible source.
//
//nolint:tagliatelle // snake_case yaml keys per project convention.
type Bucket struct {
	Name          string        `mapstructure:"name"          yaml:"name"`
	Endpoint      string        `mapstructure:"endpoint"      yaml:"endpoint"`
	Region        string        `mapstructure:"region"        yaml:"region"`
	Bucket        string        `mapstructure:"bucket"        yaml:"bucket"`
	AccessKey     string        `mapstructure:"access_key"    yaml:"access_key"`
	SecretKey     string        `mapstructure:"secret_key"    yaml:"secret_key"`
	Prefix        string        `mapstructure:"prefix"        yaml:"prefix"`
	UseSSL        bool          `default:"true"               mapstructure:"use_ssl"  yaml:"use_ssl"`
	DeleteAfter   bool          `mapstructure:"delete_after"  yaml:"delete_after"`
	PollInterval  time.Duration `mapstructure:"poll_interval" yaml:"poll_interval"`
	ObjectPattern string        `mapstructure:"pattern"       yaml:"pattern"`
	MaxObjectSize int64         `default:"1073741824"         mapstructure:"max_size" yaml:"max_size"`
}
