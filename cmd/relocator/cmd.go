// Package relocator implements the "relocator" Cobra subcommand.
package relocator

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	svc "infra.helper/cmd/relocator/internal/relocator"
	"infra.helper/pkg/app"
)

var (
	configPath string

	errInvalidConfig    = errors.New("invalid config")
	errListenRequired   = errors.New("listen is required")
	errDBPathRequired   = errors.New("db_path is required")
	errWorkDirRequired  = errors.New("work_dir is required")
	errBucketsRequired  = errors.New("at least one bucket is required")
	errPostURLRequired  = errors.New("post.url is required")
	errBucketIncomplete = errors.New("bucket has missing required fields")
)

var relocatorCmd = &cobra.Command{
	Use:   "relocator",
	Short: "Скачивает архивы из S3, распаковывает и POST'ит JSON-файлы",
	Long: "relocator периодически опрашивает S3-совместимые бакеты разных провайдеров, " +
		"скачивает новые архивы, распаковывает (включая запароленные zip — пароли берутся из " +
		"конфига), отправляет JSON-файлы POST-запросом на указанный URL, опционально удаляет " +
		"объекты из источника, ведёт статистику в embedded BoltDB и публикует встроенную " +
		"страницу состояния.",
	Run: func(_ *cobra.Command, _ []string) {
		ctx, onStop := app.AddJob("relocator")
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
			Str("listen", cfg.Listen).
			Str("db", cfg.DBPath).
			Int("buckets", len(cfg.Buckets)).
			Int("passwords", len(cfg.Passwords)).
			Msg("relocator starting")

		startErr := svc.Start(ctx, cfg)
		if startErr != nil {
			log.Error().Err(startErr).Msg("relocator start failed")

			return
		}

		<-ctx.Done()
	},
}

// Register attaches the subcommand to the root.
func Register(parent *cobra.Command) {
	parent.AddCommand(relocatorCmd)
}

func init() {
	relocatorCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "configuration file path")
}

func validateConfig(cfg svc.Config) error {
	if cfg.Listen == "" {
		return errListenRequired
	}

	if cfg.DBPath == "" {
		return errDBPathRequired
	}

	if cfg.WorkDir == "" {
		return errWorkDirRequired
	}

	if cfg.Post.URL == "" {
		return errPostURLRequired
	}

	if len(cfg.Buckets) == 0 {
		return errBucketsRequired
	}

	seen := make(map[string]struct{}, len(cfg.Buckets))

	for idx, bcfg := range cfg.Buckets {
		if bcfg.Name == "" || bcfg.Endpoint == "" || bcfg.Bucket == "" {
			return fmt.Errorf("%w: buckets[%d]", errBucketIncomplete, idx)
		}

		if _, ok := seen[bcfg.Name]; ok {
			return fmt.Errorf("%w: duplicate bucket name %q", errInvalidConfig, bcfg.Name)
		}

		seen[bcfg.Name] = struct{}{}
	}

	return nil
}
