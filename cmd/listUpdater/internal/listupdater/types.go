package listupdater

import "time"

// Config is the YAML config for the list-updater service.
type Config struct {
	Dir     string        `default:"./data"     mapstructure:"dir"     yaml:"dir"`
	Listen  string        `default:":8080"      mapstructure:"listen"  yaml:"listen"`
	Refresh time.Duration `default:"1h"         mapstructure:"refresh" yaml:"refresh"`
	Lists   []List        `mapstructure:"lists" yaml:"lists"`
}

// List describes a downloadable list.
type List struct {
	Name string `mapstructure:"name" yaml:"name"`
	URL  string `mapstructure:"url"  yaml:"url"`
	// "" - none
	// basic - AuthData - user:password
	// token - AuthData - token
	AuthType string `mapstructure:"authType" yaml:"authType"`
	AuthData any    `mapstructure:"authData" yaml:"authData"`
}
