// Package cmd wires the Cobra CLI commands.
package cmd

import (
	"os"
	"time"

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

const (
	defaultMetricsAddr = ":9101"
	metricsUsage       = "адрес для экспорта метрик (по умолчанию :9101 при указании без значения)"
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "infra-helper",
	Short: "Инструмент поддержки инфраструктуры",
	Run: func(cmd *cobra.Command, _ []string) {
		// Если нет подкоманды и установлен флаг --version
		if version {
			app.LogVersion()
			os.Exit(0)
		}

		// Если флаг --version не указан, выводим хелп
		_ = cmd.Help()
		// app.WG().Done()
	},
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		app.Init(debug)

		if version {
			app.LogVersion()
			os.Exit(0)
		}

		if len(metrics) == 0 {
			noMetrics = true
		}
		// Обработка флагов --metrics и --no-metrics
		if noMetrics {
			log.Info().Msg("экспорт метрик отключён.")

			metrics = "" // Полностью отключаем метрики
		} else {
			if !cmd.Flags().Changed("metrics") && metrics == "" {
				metrics = defaultMetricsAddr
			}

			if metrics != "" {
				os.Setenv("METRICS", metrics)
				app.SetName(cmd.CommandPath())
				go app.StartMetrics(app.AddJob("metrics"))
			}
		}
	},
	PersistentPostRun: func(_ *cobra.Command, _ []string) {
		time.Sleep(time.Second)
		// 	log.Info().Msg("waiting for completion")
		// 	log.Info().Msg("Дождались 123")
		app.WG().Wait()
		// 	log.Info().Msg("Дождались")
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
	rootCmd.PersistentFlags().StringVar(&metrics, "metrics", "", metricsUsage)
	rootCmd.PersistentFlags().BoolVar(&noMetrics, "no-metrics", false, "отключить экспорт метрик")

	listupdater.Register(rootCmd)
}
