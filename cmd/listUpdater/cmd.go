// Package listupdater implements the "list-updater" Cobra subcommand.
package listupdater

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	svc "infra.helper/cmd/listUpdater/internal/listupdater"
	"infra.helper/pkg/app"
)

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

		var cfg svc.Config

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

		svc.Start(cfg)

		<-ctx.Done()
	},
}

// Register registers the list-updater command.
func Register(parent *cobra.Command) {
	parent.AddCommand(listUpdaterCmd)
}

func init() {
	listUpdaterCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "configuration file path")
}

func validateConfig(cfg svc.Config) error {
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
