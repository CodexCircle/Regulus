package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "regulus.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(nil, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Fatalf("got %+v, want defaults %+v", cfg, Default())
	}
}

func TestPrecedence(t *testing.T) {
	file := writeFile(t, "listen: ':1111'\ndata_dir: /from/file\nlog:\n  level: debug\n")

	// file only
	cfg, err := Load([]string{"-config", file}, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":1111" || cfg.DataDir != "/from/file" || cfg.Log.Level != "debug" || cfg.Log.Format != "text" {
		t.Fatalf("file values not applied correctly: %+v", cfg)
	}

	// environment overrides the file
	e := env(map[string]string{"REGULUS_LISTEN": ":2222", "REGULUS_LOG_FORMAT": "JSON"})
	cfg, err = Load([]string{"-config", file}, e, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":2222" || cfg.DataDir != "/from/file" || cfg.Log.Format != "json" {
		t.Fatalf("env values not applied correctly: %+v", cfg)
	}

	// flags override the environment
	cfg, err = Load([]string{"-config", file, "-listen", ":3333"}, e, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":3333" {
		t.Fatalf("flag not applied: %+v", cfg)
	}
}

func TestConfigPathFromEnv(t *testing.T) {
	file := writeFile(t, "data_dir: /from/env-file\n")
	cfg, err := Load(nil, env(map[string]string{"REGULUS_CONFIG": file}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/from/env-file" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		file string
		want string
	}{
		{"unknown key", []string{"-config", "@"}, nil, "listn: ':1'\n", "listn"},
		{"missing file", []string{"-config", "/does/not/exist.yaml"}, nil, "", "read config file"},
		{"bad level", []string{"-log-level", "loud"}, nil, "", "log.level"},
		{"bad format", nil, map[string]string{"REGULUS_LOG_FORMAT": "xml"}, "", "log.format"},
		{"unknown flag", []string{"-nope"}, nil, "", "nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args
			if tt.file != "" {
				p := writeFile(t, tt.file)
				for i, a := range args {
					if a == "@" {
						args[i] = p
					}
				}
			}
			_, err := Load(args, env(tt.env), io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
