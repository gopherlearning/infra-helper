package app

import (
	"github.com/rs/zerolog/log"
)

var (
	buildVersion = "N/A"
	buildDate    = "N/A"
	buildCommit  = "N/A"
)

// LogVersion logs build version information.
func LogVersion() {
	log.Info().
		Str("version", buildVersion).
		Str("date", buildDate).
		Str("commit", buildCommit).
		Msg("")
}
