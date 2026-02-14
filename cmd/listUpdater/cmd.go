// Package listupdater implements the "list-updater" Cobra subcommand.
package listupdater

import (
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"infra.helper/pkg/app"
)

type config struct {
	Dir     string        `default:"./data"     mapstructure:"dir"     yaml:"dir"`
	Listen  string        `default:":8080"      mapstructure:"listen"  yaml:"listen"`
	Refresh time.Duration `default:"1h"         mapstructure:"refresh" yaml:"refresh"`
	Lists   []list        `mapstructure:"lists" yaml:"lists"`
}

type list struct {
	Name string `mapstructure:"name" yaml:"name"`
	URL  string `mapstructure:"url"  yaml:"url"`
	// "" - none
	// basic - AuthData - user:password
	// token - AuthData - token
	AuthType string `mapstructure:"authType" yaml:"authType"`
	AuthData any    `mapstructure:"authData" yaml:"authData"`
}

var (
	configPath        string
	errInvalidConfig  = errors.New("invalid config")
	errListenRequired = errors.New("listen is required")
	errDirRequired    = errors.New("dir is required")
	errListsRequired  = errors.New("lists is required")
)

// listUpdaterCmd represents the listUpdater command.
var listUpdaterCmd = &cobra.Command{
	Use:   "list-updater",
	Short: "Кеширующий прокси для обновляемых листов",
	Run: func(_ *cobra.Command, _ []string) {
		ctx, onStop := app.AddJob("listUpdater")
		defer onStop()

		var cfg config

		err := app.ReadFromFile(configPath, &cfg)
		if err != nil {
			log.Error().Err(err).Msg("config broken")

			return
		}

		validateErr := validateConfig(cfg)
		if validateErr != nil {
			log.Error().Err(validateErr).Msg("invalid config")

			return
		}

		log.Info().
			Str("listen", cfg.Listen).
			Str("dir", cfg.Dir).
			Dur("refresh", cfg.Refresh).
			Int("lists", len(cfg.Lists)).
			Msg("list-updater started")

		startListUpdater(cfg)

		<-ctx.Done()
	},
}

// Register registers the list-updater command.
func Register(parent *cobra.Command) {
	parent.AddCommand(listUpdaterCmd)
}

func init() {
	listUpdaterCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "configuration file path")
	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// listUpdaterCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// listUpdaterCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func validateConfig(cfg config) error {
	if cfg.Listen == "" {
		return errListenRequired
	}

	if cfg.Dir == "" {
		return errDirRequired
	}

	if cfg.Refresh <= 0 {
		return fmt.Errorf("%w: refresh must be > 0", errInvalidConfig)
	}

	if len(cfg.Lists) == 0 {
		return errListsRequired
	}

	seen := make(map[string]struct{}, len(cfg.Lists))
	for idx, lst := range cfg.Lists {
		if lst.Name == "" {
			return fmt.Errorf("%w: lists[%d].name is required", errInvalidConfig, idx)
		}

		if lst.URL == "" {
			return fmt.Errorf("%w: lists[%d].url is required", errInvalidConfig, idx)
		}

		if _, ok := seen[lst.Name]; ok {
			return fmt.Errorf("%w: duplicate list name: %s", errInvalidConfig, lst.Name)
		}

		seen[lst.Name] = struct{}{}
	}

	return nil
}
