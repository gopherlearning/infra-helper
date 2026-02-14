package app

import (
	"github.com/rs/zerolog/log"
)

type Version bool

var (
	buildVersion = "N/A"
	buildDate    = "N/A"
	buildCommit  = "N/A"
)

// IsDebugMode возвращает true, если активирован режим отладки.
func LogVersion() {
	log.Info().
		Str("version", buildVersion).
		Str("date", buildDate).
		Str("commit", buildCommit).
		Msg("")
}
