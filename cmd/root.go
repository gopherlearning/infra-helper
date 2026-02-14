/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	listupdater "infra.helper/cmd/listUpdater"
	"infra.helper/pkg/app"
)

var (
	debug     bool
	version   bool
	metrics   string
	noMetrics bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "infra.helper",
	Short: "Инструмент поддержки инфраструктуры",
	Run: func(cmd *cobra.Command, args []string) {
		// Если нет подкоманды и установлен флаг --version
		if version {
			app.LogVersion()
			os.Exit(0)
		}

		// Если флаг --version не указан, выводим хелп
		_ = cmd.Help()
		// app.WG().Done()
	},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		app.Init(debug)

		if version {
			app.LogVersion()
			os.Exit(0)
		}

		// Обработка флагов --metrics и --no-metrics
		if noMetrics {
			log.Info().Msg("экспорт метрик отключён.")
			metrics = "" // Полностью отключаем метрики
		} else {
			if !cmd.Flags().Changed("metrics") && metrics == "" {
				metrics = ":9101" // Значение по умолчанию
			}

			// if metrics != "" {
			// 	app.StartMetrics(app.AddJob("metrics"), metrics)
			// 	log.Info().Msgf("метрики доступны по адресу: %s", metrics)
			// }
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// app.Init()
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.infra.helper.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "v", false, "включить debug-режим")
	rootCmd.PersistentFlags().BoolVar(&version, "version", false, "показать версию приложения")
	rootCmd.PersistentFlags().StringVar(&metrics, "metrics", "", "адрес для экспорта метрик (по умолчанию :9001 при указании без значения)")
	rootCmd.PersistentFlags().BoolVar(&noMetrics, "no-metrics", false, "отключить экспорт метрик")

	rootCmd.AddCommand(listupdater.ListUpdaterCmd)
}
