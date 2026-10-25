package console

import (
	"strings"
	"testing"
)

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func ptr(b bool) *bool { return &b }

var tty = Terminal{Interactive: true, Width: 120, Height: 40}

func TestUseTUI(t *testing.T) {
	for _, tc := range []struct {
		name      string
		choice    Choice
		configTUI bool
		env       func(string) string
		term      Terminal
		want      bool
		reason    string
	}{
		{"a terminal opens it", Choice{}, true, env(), tty, true, ""},
		{"--no-tui", Choice{TUI: ptr(false)}, true, env(), tty, false, "--no-tui"},
		{"--no-input", Choice{NoInput: true}, true, env(), tty, false, "--no-input"},
		{"--json", Choice{JSON: true}, true, env(), tty, false, "plain or JSON"},
		{"--plain", Choice{Plain: true}, true, env(), tty, false, "plain or JSON"},
		{"REGULUS_NO_TUI", Choice{}, true, env("REGULUS_NO_TUI", "1"), tty, false, "REGULUS_NO_TUI"},
		{"REGULUS_NO_TUI=0 does not count", Choice{}, true, env("REGULUS_NO_TUI", "0"), tty, true, ""},
		{"CI", Choice{}, true, env("CI", "true"), tty, false, "CI"},
		{"CI=false does not count", Choice{}, true, env("CI", "false"), tty, true, ""},
		{"TERM=dumb", Choice{}, true, env("TERM", "dumb"), tty, false, "TERM"},
		{"console.tui false", Choice{}, false, env(), tty, false, "console.tui"},
		{"stdout is a pipe", Choice{}, true, env(), Terminal{}, false, "not running in a terminal"},
		{"narrow terminal", Choice{}, true, env(), Terminal{Interactive: true, Width: 40, Height: 30}, false, "too small"},
		{"short terminal", Choice{}, true, env(), Terminal{Interactive: true, Width: 100, Height: 8}, false, "too small"},
		{"size unknown is not too small", Choice{}, true, env(), Terminal{Interactive: true}, true, ""},

		// Precedence: flag beats environment beats configuration beats detection.
		{"--tui beats the environment", Choice{TUI: ptr(true)}, true, env("REGULUS_NO_TUI", "1", "CI", "1", "TERM", "dumb"), tty, true, ""},
		{"--tui beats the configuration", Choice{TUI: ptr(true)}, false, env(), tty, true, ""},
		{"--tui beats detection", Choice{TUI: ptr(true)}, true, env(), Terminal{}, true, ""},
		{"--no-tui beats everything", Choice{TUI: ptr(false)}, true, env(), tty, false, "--no-tui"},
		{"the environment beats the configuration", Choice{}, false, env("CI", "1"), tty, false, "CI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, reason, err := UseTUI(tc.choice, tc.configTUI, tc.env, tc.term)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("UseTUI = %v (%s), want %v", got, reason, tc.want)
			}
			if !tc.want && !strings.Contains(reason, tc.reason) {
				t.Errorf("reason = %q, want it to mention %q", reason, tc.reason)
			}
		})
	}
}

func TestTUIConflictsWithPlainOutput(t *testing.T) {
	for _, c := range []Choice{{TUI: ptr(true), NoInput: true}, {TUI: ptr(true), JSON: true}, {TUI: ptr(true), Plain: true}} {
		if _, _, err := UseTUI(c, true, env(), tty); err == nil {
			t.Errorf("%+v was accepted", c)
		}
	}
}
