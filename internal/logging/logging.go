// Package logging builds the structured logger used across Regulus.
package logging

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/CodexCircle/Regulus/internal/config"
)

// New returns a logger writing to w with the level and format from cfg.
func New(cfg config.Log, w io.Writer) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return nil, fmt.Errorf("log level %q: %w", cfg.Level, err)
	}
	opts := &slog.HandlerOptions{Level: level}

	switch cfg.Format {
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("log format %q is not supported", cfg.Format)
	}
}
