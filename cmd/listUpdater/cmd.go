package listupdater

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"infra.helper/pkg/app"
)

// listUpdaterCmd represents the listUpdater command
var ListUpdaterCmd = &cobra.Command{
	Use:   "list-updater",
	Short: "Кеширующий прокси для обновляемых листов",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, onStop := app.AddJob("listUpdater")
		defer onStop()

		fmt.Println("listUpdater called")

		go func() {
			time.Sleep(2 * time.Second)
			app.Cancel()
		}()
		<-ctx.Done()
	},
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
