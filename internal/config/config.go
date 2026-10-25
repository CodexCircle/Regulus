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
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the complete hub configuration.
type Config struct {
	// Listen is the address the HTTP server listens on.
	Listen string `yaml:"listen"`
	// DataDir is the directory where the hub stores its database.
	DataDir string `yaml:"data_dir"`
	// BuiltinAgent runs an agent inside the hub, so the hub's own server is
	// managed like any other.
	BuiltinAgent bool `yaml:"builtin_agent"`
	// MetricsInterval is how often agents report their metrics to the hub.
	MetricsInterval time.Duration `yaml:"metrics_interval"`
	Log             Log           `yaml:"log"`
}

// MinMetricsInterval is the shortest accepted metrics interval.
const MinMetricsInterval = time.Second

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
		Listen:          ":8080",
		DataDir:         "data",
		BuiltinAgent:    true,
		MetricsInterval: 10 * time.Second,
		Log:             Log{Level: "info", Format: "text"},
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
	if c.MetricsInterval < MinMetricsInterval {
		return fmt.Errorf("metrics_interval %v is too short (minimum %v)", c.MetricsInterval, MinMetricsInterval)
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
	return LoadWith("hub", args, getenv, output, nil)
}

// LoadWith is Load for commands that take flags of their own besides the
// configuration ones: name is shown in the usage, and extra defines the other
// flags on the flag set before the arguments are parsed.
func LoadWith(name string, args []string, getenv func(string) string, output io.Writer, extra func(*flag.FlagSet)) (Config, error) {
	cfg := Default()

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(output)
	if extra != nil {
		extra(fs)
	}
	path := fs.String("config", "", "path to the YAML configuration file (env REGULUS_CONFIG)")
	listen := fs.String("listen", "", "address to listen on (env REGULUS_LISTEN)")
	dataDir := fs.String("data-dir", "", "directory for the hub data (env REGULUS_DATA_DIR)")
	builtinAgent := fs.Bool("builtin-agent", true, "run an agent inside the hub (env REGULUS_BUILTIN_AGENT)")
	metricsInterval := fs.Duration("metrics-interval", 0, "how often agents report metrics, for example 10s (env REGULUS_METRICS_INTERVAL)")
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
	if v := getenv("REGULUS_BUILTIN_AGENT"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid configuration: REGULUS_BUILTIN_AGENT %q is not a boolean", v)
		}
		cfg.BuiltinAgent = b
	}
	if v := getenv("REGULUS_METRICS_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid configuration: REGULUS_METRICS_INTERVAL %q is not a duration (for example 10s)", v)
		}
		cfg.MetricsInterval = d
	}
	setIfNotEmpty(&cfg.Log.Level, getenv("REGULUS_LOG_LEVEL"))
	setIfNotEmpty(&cfg.Log.Format, getenv("REGULUS_LOG_FORMAT"))

	// Flags win over everything else, but only when explicitly given.
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "listen":
			cfg.Listen = *listen
		case "data-dir":
			cfg.DataDir = *dataDir
		case "builtin-agent":
			cfg.BuiltinAgent = *builtinAgent
		case "metrics-interval":
			cfg.MetricsInterval = *metricsInterval
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
