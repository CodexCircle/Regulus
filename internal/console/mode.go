// Package console is the interactive terminal console of Regulus and the
// plain-text and JSON output that takes its place when no terminal UI is
// wanted: what the hub is doing, its settings, its logs and health checks.
package console

import (
	"errors"
	"strings"
)

// Terminal describes the terminal the command runs in.
type Terminal struct {
	// Interactive is true when both stdin and stdout are terminals.
	Interactive   bool
	Width, Height int
}

// Minimum size for the full-screen console; below it the plain output is used.
const (
	MinWidth  = 60
	MinHeight = 16
)

// Choice is what the person asked on the command line.
type Choice struct {
	// TUI is true for --tui, false for --no-tui, nil when neither was given.
	TUI *bool
	// NoInput is --no-input: never ask anything, so no interactive screen.
	NoInput bool
	// Plain and JSON are the other output formats; they print instead of
	// opening the screen.
	Plain, JSON bool
}

// UseTUI decides whether to open the full-screen console and says why not
// when it does not. The order is: command-line flags, then environment
// variables, then the configuration file (console.tui), then what the
// terminal looks like.
func UseTUI(c Choice, configTUI bool, getenv func(string) string, term Terminal) (use bool, reason string, err error) {
	if c.TUI != nil && *c.TUI && (c.NoInput || c.Plain || c.JSON) {
		return false, "", errors.New("--tui cannot be combined with --no-input, --plain or --json")
	}
	switch {
	case c.TUI != nil && *c.TUI:
		return true, "", nil
	case c.TUI != nil:
		return false, "--no-tui", nil
	case c.NoInput:
		return false, "--no-input", nil
	case c.Plain || c.JSON:
		return false, "plain or JSON output was asked for", nil
	}

	if truthy(getenv("REGULUS_NO_TUI")) {
		return false, "REGULUS_NO_TUI is set", nil
	}
	if v := getenv("CI"); v != "" && !falsy(v) {
		return false, "CI is set", nil
	}
	if getenv("TERM") == "dumb" {
		return false, "TERM is dumb", nil
	}

	if !configTUI {
		return false, "console.tui is false in the configuration", nil
	}

	if !term.Interactive {
		return false, "not running in a terminal", nil
	}
	if term.Width > 0 && term.Width < MinWidth || term.Height > 0 && term.Height < MinHeight {
		return false, "the terminal is too small", nil
	}
	return true, "", nil
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func falsy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

// UnicodeTerminal guesses whether the terminal can show characters beyond
// ASCII (✓, ●, box lines) from the locale; without it the console draws with
// plain characters.
func UnicodeTerminal(getenv func(string) string) bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := getenv(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}
