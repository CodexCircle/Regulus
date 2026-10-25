package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestBuiltinAgent(t *testing.T) {
	file := writeFile(t, "builtin_agent: false\n")

	cfg, err := Load(nil, env(nil), io.Discard)
	if err != nil || !cfg.BuiltinAgent {
		t.Fatalf("default: cfg = %+v, err = %v; want the built-in agent enabled", cfg, err)
	}

	cfg, err = Load([]string{"-config", file}, env(nil), io.Discard)
	if err != nil || cfg.BuiltinAgent {
		t.Fatalf("file: cfg = %+v, err = %v; want the built-in agent disabled", cfg, err)
	}

	// environment overrides the file, flags override the environment
	cfg, err = Load([]string{"-config", file}, env(map[string]string{"REGULUS_BUILTIN_AGENT": "true"}), io.Discard)
	if err != nil || !cfg.BuiltinAgent {
		t.Fatalf("env: cfg = %+v, err = %v; want the built-in agent enabled", cfg, err)
	}
	cfg, err = Load([]string{"-builtin-agent=false"}, env(map[string]string{"REGULUS_BUILTIN_AGENT": "true"}), io.Discard)
	if err != nil || cfg.BuiltinAgent {
		t.Fatalf("flag: cfg = %+v, err = %v; want the built-in agent disabled", cfg, err)
	}

	if _, err := Load(nil, env(map[string]string{"REGULUS_BUILTIN_AGENT": "maybe"}), io.Discard); err == nil {
		t.Fatal("expected an error for a value that is not a boolean")
	}
}

func TestMetricsInterval(t *testing.T) {
	file := writeFile(t, "metrics_interval: 30s\n")

	cfg, err := Load(nil, env(nil), io.Discard)
	if err != nil || cfg.MetricsInterval != 10*time.Second {
		t.Fatalf("default: cfg = %+v, err = %v; want 10s", cfg, err)
	}
	cfg, err = Load([]string{"-config", file}, env(nil), io.Discard)
	if err != nil || cfg.MetricsInterval != 30*time.Second {
		t.Fatalf("file: cfg = %+v, err = %v; want 30s", cfg, err)
	}
	cfg, err = Load([]string{"-config", file}, env(map[string]string{"REGULUS_METRICS_INTERVAL": "20s"}), io.Discard)
	if err != nil || cfg.MetricsInterval != 20*time.Second {
		t.Fatalf("env: cfg = %+v, err = %v; want 20s", cfg, err)
	}
	cfg, err = Load([]string{"-metrics-interval", "5s"}, env(map[string]string{"REGULUS_METRICS_INTERVAL": "20s"}), io.Discard)
	if err != nil || cfg.MetricsInterval != 5*time.Second {
		t.Fatalf("flag: cfg = %+v, err = %v; want 5s", cfg, err)
	}

	for name, args := range map[string][]string{"too short": {"-metrics-interval", "100ms"}, "zero": {"-metrics-interval", "0"}} {
		if _, err := Load(args, env(nil), io.Discard); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := Load(nil, env(map[string]string{"REGULUS_METRICS_INTERVAL": "soon"}), io.Discard); err == nil {
		t.Error("expected an error for a value that is not a duration")
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

func TestPackagedConfigIsValid(t *testing.T) {
	// The file installed by the .deb (packaging/config.yaml) must load.
	path := filepath.Join("..", "..", "packaging", "config.yaml")
	cfg, err := Load([]string{"--config", path}, func(string) string { return "" }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/var/lib/regulus" || !cfg.BuiltinAgent || cfg.Listen != ":8080" {
		t.Errorf("unexpected packaged defaults: %+v", cfg)
	}
}

func TestAgentListen(t *testing.T) {
	cfg, err := Load(nil, env(nil), io.Discard)
	if err != nil || cfg.AgentListen != "" {
		t.Fatalf("default = %q, %v; want remote agents off", cfg.AgentListen, err)
	}
	cfg, err = Load(nil, env(map[string]string{"REGULUS_AGENT_LISTEN": ":8443"}), io.Discard)
	if err != nil || cfg.AgentListen != ":8443" {
		t.Errorf("from env = %q, %v", cfg.AgentListen, err)
	}
	cfg, err = Load([]string{"--agent-listen", "127.0.0.1:9000"}, env(map[string]string{"REGULUS_AGENT_LISTEN": ":8443"}), io.Discard)
	if err != nil || cfg.AgentListen != "127.0.0.1:9000" {
		t.Errorf("flag over env = %q, %v", cfg.AgentListen, err)
	}
}

func TestLoadAgent(t *testing.T) {
	cfg, err := LoadAgent([]string{"--hub", "hub.example:8443"}, env(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hub != "hub.example:8443" || cfg.DataDir != "agent-data" || cfg.Log.Level != "info" {
		t.Errorf("defaults = %+v", cfg)
	}

	file := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(file, []byte("hub: file.example:1\ndata_dir: /var/lib/regulus-agent\nca_fingerprint: sha256:abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadAgent([]string{"--config", file, "--token", "from-flag"}, env(map[string]string{
		"REGULUS_AGENT_HUB":   "env.example:2",
		"REGULUS_AGENT_TOKEN": "from-env",
	}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// file < env < flag
	if cfg.Hub != "env.example:2" || cfg.Token != "from-flag" || cfg.DataDir != "/var/lib/regulus-agent" || cfg.CAFingerprint != "sha256:abc" {
		t.Errorf("precedence = %+v", cfg)
	}
}

func TestLoadAgentErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"no hub":           {},
		"hub without port": {"--hub", "hub.example"},
		"empty data dir":   {"--hub", "h:1", "--data-dir", ""},
		"bad log level":    {"--hub", "h:1", "--log-level", "loud"},
		"unknown flag":     {"--hub", "h:1", "--nope"},
	} {
		if _, err := LoadAgent(args, env(nil), io.Discard); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	_ = os.WriteFile(bad, []byte("hub: h:1\ntypo: x\n"), 0o600)
	if _, err := LoadAgent([]string{"--config", bad}, env(nil), io.Discard); err == nil {
		t.Error("an unknown key in the file must be rejected")
	}
}
