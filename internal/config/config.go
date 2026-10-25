// Package config loads the hub configuration. Sources are applied in this
// order, each one overriding the previous: built-in defaults, configuration
// file (YAML), environment variables, command-line flags.
package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the complete hub configuration.
type Config struct {
	// Listen is the address the HTTP server listens on.
	Listen string `yaml:"listen"`
	// DataDir is the directory where the hub stores its database.
	DataDir string `yaml:"data_dir"`
	Log     Log    `yaml:"log"`
}

// Log configures logging.
type Log struct {
	// Level is one of debug, info, warn, error.
	Level string `yaml:"level"`
	// Format is one of text, json.
	Format string `yaml:"format"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Listen:  ":8080",
		DataDir: "data",
		Log:     Log{Level: "info", Format: "text"},
	}
}

// Validate reports whether the configuration is usable.
func (c Config) Validate() error {
	if c.Listen == "" {
		return errors.New("listen must not be empty")
	}
	if c.DataDir == "" {
		return errors.New("data_dir must not be empty")
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level %q is invalid (want debug, info, warn or error)", c.Log.Level)
	}
	switch c.Log.Format {
	case "text", "json":
	default:
		return fmt.Errorf("log.format %q is invalid (want text or json)", c.Log.Format)
	}
	return nil
}

// Load builds the configuration from args (the command-line flags without
// the program and subcommand names) and the environment. getenv is usually
// os.Getenv. Flag usage and parse errors are written to output.
func Load(args []string, getenv func(string) string, output io.Writer) (Config, error) {
	cfg := Default()

	fs := flag.NewFlagSet("hub", flag.ContinueOnError)
	fs.SetOutput(output)
	path := fs.String("config", "", "path to the YAML configuration file (env REGULUS_CONFIG)")
	listen := fs.String("listen", "", "address to listen on (env REGULUS_LISTEN)")
	dataDir := fs.String("data-dir", "", "directory for the hub data (env REGULUS_DATA_DIR)")
	logLevel := fs.String("log-level", "", "log level: debug, info, warn, error (env REGULUS_LOG_LEVEL)")
	logFormat := fs.String("log-format", "", "log format: text, json (env REGULUS_LOG_FORMAT)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	file := *path
	if file == "" {
		file = getenv("REGULUS_CONFIG")
	}
	if file != "" {
		if err := loadFile(file, &cfg); err != nil {
			return Config{}, err
		}
	}

	setIfNotEmpty(&cfg.Listen, getenv("REGULUS_LISTEN"))
	setIfNotEmpty(&cfg.DataDir, getenv("REGULUS_DATA_DIR"))
	setIfNotEmpty(&cfg.Log.Level, getenv("REGULUS_LOG_LEVEL"))
	setIfNotEmpty(&cfg.Log.Format, getenv("REGULUS_LOG_FORMAT"))

	// Flags win over everything else, but only when explicitly given.
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "listen":
			cfg.Listen = *listen
		case "data-dir":
			cfg.DataDir = *dataDir
		case "log-level":
			cfg.Log.Level = *logLevel
		case "log-format":
			cfg.Log.Format = *logFormat
		}
	})

	cfg.Log.Level = strings.ToLower(cfg.Log.Level)
	cfg.Log.Format = strings.ToLower(cfg.Log.Format)
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject typos instead of silently ignoring them
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}
	return nil
}

func setIfNotEmpty(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}
