package listupdater

import (
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"infra.helper/pkg/app"
)

type config struct {
	Lists []list
}

type list struct {
	URL string
	// "" - none
	// basic - AuthData - user:password
	// token - AuthData - token
	AuthType string
	AuthData interface{}
}

// listUpdaterCmd represents the listUpdater command.
var listUpdaterCmd = &cobra.Command{
	Use:   "list-updater",
	Short: "Кеширующий прокси для обновляемых листов",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, onStop := app.AddJob("listUpdater")
		defer onStop()

		var cfg config
		err := app.ReadFromFile("config.yaml", &cfg)
		if err != nil {
			log.Error().Err(err).Msg("config broken")

			return
		}

		log.Info().Msg("listUpdater called")

		go func() {
			time.Sleep(2 * time.Second)
			app.Cancel()
		}()
		<-ctx.Done()
	},
}

func Register(parent *cobra.Command) {
	parent.AddCommand(listUpdaterCmd)
}

func init() {

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// listUpdaterCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// listUpdaterCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
