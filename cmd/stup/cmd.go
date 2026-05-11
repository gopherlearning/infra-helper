// Package stup implements the "stup" Cobra subcommand: a static stub sign-in page.
package stup

import (
	"errors"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	svc "infra.helper/cmd/stup/internal/stup"
	"infra.helper/pkg/app"
)

const defaultListen = "127.0.0.1:1516"

var (
	listenAddr string
	install    bool

	errListenRequired = errors.New("listen is required")
)

var stupCmd = &cobra.Command{
	Use:   "stup",
	Short: "Статичная страница-заглушка с формой входа",
	Run: func(_ *cobra.Command, _ []string) {
		if listenAddr == "" {
			log.Error().Err(errListenRequired).Msg("invalid args")

			return
		}

		if install {
			installErr := svc.Install(listenAddr)
			if installErr != nil {
				log.Error().Err(installErr).Msg("install failed")
			}

			app.Cancel()

			return
		}

		ctx, onStop := app.AddJob("stup")
		defer onStop()

		log.Info().Str("listen", listenAddr).Msg("stup started")

		svc.Start(listenAddr)

		<-ctx.Done()
	},
}

// Register registers the stup command on the parent.
func Register(parent *cobra.Command) {
	parent.AddCommand(stupCmd)
}

func init() {
	stupCmd.Flags().StringVar(&listenAddr, "listen", defaultListen, "адрес HTTP-листенера")
	stupCmd.Flags().BoolVar(&install, "install", false, "установить как systemd-сервис в /usr/local/bin и запустить")
}
