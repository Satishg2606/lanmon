// Package logger provides a structured zerolog logger for lanmon.
package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Init creates and returns a zerolog.Logger configured with the given log level.
// Supported levels: debug, info, warn, error. Defaults to info.
func Init(level string) zerolog.Logger {
	var lvl zerolog.Level
	switch level {
	case "debug":
		lvl = zerolog.DebugLevel
	case "info":
		lvl = zerolog.InfoLevel
	case "warn":
		lvl = zerolog.WarnLevel
	case "error":
		lvl = zerolog.ErrorLevel
	default:
		lvl = zerolog.InfoLevel
	}

	return zerolog.New(
		zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		},
	).Level(lvl).With().Timestamp().Logger()
}
