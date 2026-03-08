// Package logger provides a structured zerolog logger for lanmon.
package logger

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Init creates and returns a zerolog.Logger configured with the given log level.
// If logFile is non-empty, logs are written to both stderr and the file.
// Supported levels: debug, info, warn, error. Defaults to info.
func Init(level, logFile string) zerolog.Logger {
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

	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}

	var writer io.Writer = consoleWriter

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open log file %s: %v\n", logFile, err)
		} else {
			// JSON format for the file, human-readable for console
			fileWriter := zerolog.New(f).With().Timestamp().Logger()
			writer = zerolog.MultiLevelWriter(consoleWriter, fileWriter)
		}
	}

	return zerolog.New(writer).Level(lvl).With().Timestamp().Logger()
}
