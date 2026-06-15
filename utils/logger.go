package utils

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Log is the global structured logger instance.
// In development: human-readable console output with colors.
// In production: JSON output for log aggregation.
var Log zerolog.Logger

func init() {
	// Determine environment
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = os.Getenv("GO_ENV")
	}

	level := zerolog.InfoLevel
	if env == "production" {
		level = zerolog.WarnLevel
	} else if env == "debug" {
		level = zerolog.DebugLevel
	}

	zerolog.SetGlobalLevel(level)
	zerolog.TimestampFieldName = "time"
	zerolog.LevelFieldName = "level"
	zerolog.MessageFieldName = "msg"

	if env == "production" {
		// JSON output for production log aggregation
		Log = zerolog.New(os.Stdout).With().Timestamp().Logger()
	} else {
		// Human-friendly console output for development
		output := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
		Log = zerolog.New(output).With().Timestamp().Logger()
	}
}

// LogWithUser returns a logger pre-bound with a user_id field for request-scoped logging.
func LogWithUser(userID string) zerolog.Logger {
	return Log.With().Str("user_id", userID).Logger()
}

// LogWithComponent returns a logger pre-bound with a component field for subsystem logging.
func LogWithComponent(component string) zerolog.Logger {
	return Log.With().Str("component", component).Logger()
}
