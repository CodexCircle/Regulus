package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/storage"
)

// defaultEnvFile is where the package keeps the variables of the service,
// among them the database password.
const defaultEnvFile = "/etc/regulus/regulus.env"

// runDB implements `regulus db`.
func runDB(args []string) int {
	return dbCommand(args, os.Getenv, os.Stdout, os.Stderr)
}

func dbCommand(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		_, _ = fmt.Fprint(stderr, `Usage: regulus db check [--config FILE] [--env-file FILE]

check   connect with the configured database settings and report the backend,
        its version, the schema version and whether the user has the
        privileges the hub needs. It changes nothing that stays and never
        prints the password. Exit status 0 when the database is usable, 1
        when it is not, 2 for a usage error.
`)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	if args[0] != "check" {
		_, _ = fmt.Fprintf(stderr, "regulus db: unknown command %q (known: check)\n", args[0])
		return 2
	}
	return dbCheck(args[1:], getenv, stdout, stderr)
}

func dbCheck(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	envFile := defaultEnvFile
	cfg, err := config.LoadWith("db check", withDefaultConfig(args, getenv), envWithFile(getenv, &envFile), stderr, func(fs *flag.FlagSet) {
		fs.StringVar(&envFile, "env-file", defaultEnvFile, "file with the service's REGULUS_* variables, read for those not in the environment")
		fs.Usage = func() {
			_, _ = fmt.Fprint(fs.Output(), "Usage: regulus db check [--config FILE] [--env-file FILE]\n\nFlags:\n")
			fs.PrintDefaults()
		}
	})
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: %v\n", err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rep, err := storage.Check(ctx, cfg.Storage())
	_, _ = fmt.Fprintf(stdout, "Database: %s\n", rep.Where)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "regulus: database check failed: %v\n", err)
		return 1
	}
	if rep.Backend == "SQLite" && !rep.Created {
		_, _ = fmt.Fprintln(stdout, "Status:   no database file yet: it is created at the first start")
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "Server:   %s %s\n", rep.Backend, rep.Version)
	if rep.Schema == 0 {
		_, _ = fmt.Fprintf(stdout, "Schema:   empty (the hub creates it at the first start; this Regulus knows up to %d)\n", rep.Known)
	} else {
		_, _ = fmt.Fprintf(stdout, "Schema:   version %d (this Regulus knows up to %d)\n", rep.Schema, rep.Known)
	}
	_, _ = fmt.Fprintln(stdout, "Status:   ok")
	return 0
}

// withDefaultConfig adds --config /etc/regulus/config.yaml when no file was
// named by flag or environment and the packaged one exists, as the console
// does.
func withDefaultConfig(args []string, getenv func(string) string) []string {
	for _, a := range args {
		if a == "--config" || a == "-config" || strings.HasPrefix(a, "--config=") || strings.HasPrefix(a, "-config=") {
			return args
		}
	}
	if f := config.FindFile("", getenv); f != "" {
		return append([]string{"--config", f}, args...)
	}
	return args
}

// envWithFile answers from the environment first and, for the variables that
// are not set there, from the service's environment file when it can be read.
// That way `sudo regulus db check` sees the password the service sees.
func envWithFile(getenv func(string) string, file *string) func(string) string {
	var loaded map[string]string
	return func(k string) string {
		if v := getenv(k); v != "" {
			return v
		}
		if loaded == nil {
			loaded = readEnvFile(*file)
		}
		return loaded[k]
	}
}

// readEnvFile reads KEY=VALUE lines (the systemd EnvironmentFile form). An
// unreadable or missing file is not an error: it gives nothing.
func readEnvFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}
