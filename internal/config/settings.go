package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultFile is where the package installs the hub configuration. The hub
// itself only reads a file it is told about (--config); the console and the
// `regulus config` commands fall back to this one when it exists.
const DefaultFile = "/etc/regulus/config.yaml"

// Setting describes one hub setting that people can read and change with the
// console or `regulus config`.
type Setting struct {
	// Key is the dotted YAML path, such as log.level.
	Key string
	// Env is the environment variable that overrides it, if any.
	Env         string
	Description string
	// Choices lists the accepted values when there is a fixed set.
	Choices []string
	// Restart is true when the hub reads the setting only at start.
	Restart bool
	// quote is true for values that are written as YAML strings.
	quote bool
}

// Settings returns the settings in the order they are shown.
func Settings() []Setting {
	return []Setting{
		{Key: "listen", Env: "REGULUS_LISTEN", Description: "Address of the dashboard and API: host:port, or only the port (8080)", Restart: true, quote: true},
		{Key: "agent_listen", Env: "REGULUS_AGENT_LISTEN", Description: "Address where remote agents connect (gRPC over TLS); empty keeps remote agents off", Restart: true, quote: true},
		{Key: "data_dir", Env: "REGULUS_DATA_DIR", Description: "Directory where the hub keeps its database and keys", Restart: true, quote: true},
		{Key: "builtin_agent", Env: "REGULUS_BUILTIN_AGENT", Description: "Run an agent inside the hub so its own server is managed too", Choices: []string{"true", "false"}, Restart: true},
		{Key: "metrics_interval", Env: "REGULUS_METRICS_INTERVAL", Description: "How often agents report metrics, such as 10s (at least 1s)", Restart: true},
		{Key: "log.level", Env: "REGULUS_LOG_LEVEL", Description: "How much the hub logs", Choices: []string{"debug", "info", "warn", "error"}, Restart: true},
		{Key: "log.format", Env: "REGULUS_LOG_FORMAT", Description: "Log format", Choices: []string{"text", "json"}, Restart: true},
		{Key: "updates.check", Env: "REGULUS_UPDATES_CHECK", Description: "Look for new releases on GitHub every few hours; false keeps the hub from contacting any server for updates", Choices: []string{"true", "false"}, Restart: true},
		{Key: "console.tui", Description: "Open the full-screen console when running `regulus` in a terminal", Choices: []string{"true", "false"}},
	}
}

// Lookup returns the setting with the given key.
func Lookup(key string) (Setting, bool) {
	for _, s := range Settings() {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

// Keys lists the keys of the settings.
func Keys() []string {
	var keys []string
	for _, s := range Settings() {
		keys = append(keys, s.Key)
	}
	return keys
}

// Get returns the value of a setting in the form Set accepts.
func (c Config) Get(key string) (string, bool) {
	switch key {
	case "listen":
		return c.Listen, true
	case "agent_listen":
		return c.AgentListen, true
	case "data_dir":
		return c.DataDir, true
	case "builtin_agent":
		return strconv.FormatBool(c.BuiltinAgent), true
	case "metrics_interval":
		return c.MetricsInterval.String(), true
	case "log.level":
		return c.Log.Level, true
	case "log.format":
		return c.Log.Format, true
	case "updates.check":
		return strconv.FormatBool(c.Updates.Check), true
	case "console.tui":
		return strconv.FormatBool(c.Console.TUI), true
	}
	return "", false
}

// Normalize checks a value typed for a setting and returns it in canonical
// form (for example "9090" becomes ":9090"). The error says what is wrong in
// words fit for a person typing it.
func Normalize(key, raw string) (string, error) {
	if _, ok := Lookup(key); !ok {
		return "", fmt.Errorf("unknown setting %q (known: %s)", key, strings.Join(Keys(), ", "))
	}
	v := strings.TrimSpace(raw)
	switch key {
	case "listen":
		return normalizeAddress(v)
	case "agent_listen":
		if v == "" || strings.EqualFold(v, "off") {
			return "", nil
		}
		return normalizeAddress(v)
	case "data_dir":
		if v == "" {
			return "", errors.New("data_dir must not be empty")
		}
		return v, nil
	case "builtin_agent", "console.tui", "updates.check":
		b, err := strconv.ParseBool(v)
		if err != nil {
			return "", errors.New("use true or false")
		}
		return strconv.FormatBool(b), nil
	case "metrics_interval":
		d, err := time.ParseDuration(v)
		if err != nil {
			return "", errors.New("use a duration such as 10s or 1m")
		}
		if d < MinMetricsInterval {
			return "", fmt.Errorf("too short (minimum %v)", MinMetricsInterval)
		}
		return d.String(), nil
	case "log.level":
		return oneOf(v, "debug", "info", "warn", "error")
	case "log.format":
		return oneOf(v, "text", "json")
	}
	return "", fmt.Errorf("unknown setting %q", key)
}

func oneOf(v string, choices ...string) (string, error) {
	v = strings.ToLower(v)
	for _, c := range choices {
		if v == c {
			return v, nil
		}
	}
	return "", fmt.Errorf("use one of: %s", strings.Join(choices, ", "))
}

// normalizeAddress accepts host:port or a bare port.
func normalizeAddress(v string) (string, error) {
	if v == "" {
		return "", errors.New("must not be empty")
	}
	if _, err := strconv.Atoi(v); err == nil {
		v = ":" + v
	}
	host, port, err := net.SplitHostPort(v)
	if err != nil {
		return "", errors.New("use host:port, such as :8080 or 127.0.0.1:8080, or only the port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("the port must be a number from 1 to 65535")
	}
	if host == "" {
		return ":" + port, nil
	}
	return net.JoinHostPort(host, port), nil
}

// Origins of a value.
const (
	OriginDefault = "default"
	OriginFile    = "file"
	OriginEnv     = "environment"
)

// Entry is a setting with its current value and where the value comes from.
type Entry struct {
	Setting
	Value  string
	Origin string
}

// FindFile returns the configuration file to use: the explicit path, else
// REGULUS_CONFIG, else DefaultFile when it exists, else "".
func FindFile(explicit string, getenv func(string) string) string {
	if explicit != "" {
		return explicit
	}
	if v := getenv("REGULUS_CONFIG"); v != "" {
		return v
	}
	if _, err := os.Stat(DefaultFile); err == nil {
		return DefaultFile
	}
	return ""
}

// Resolve loads the configuration the way the hub would (defaults, file,
// environment) and returns it with every setting's value and origin.
func Resolve(file string, getenv func(string) string) (Config, []Entry, error) {
	var args []string
	if file != "" {
		args = []string{"--config", file}
	}
	cfg, err := Load(args, getenv, io.Discard)
	if err != nil {
		return Config{}, nil, err
	}
	inFile := map[string]bool{}
	if file != "" {
		if data, err := os.ReadFile(file); err == nil {
			var doc map[string]any
			if yaml.Unmarshal(data, &doc) == nil {
				collectKeys("", doc, inFile)
			}
		}
	}
	var entries []Entry
	for _, s := range Settings() {
		v, _ := cfg.Get(s.Key)
		e := Entry{Setting: s, Value: v, Origin: OriginDefault}
		switch {
		case s.Env != "" && getenv(s.Env) != "":
			e.Origin = OriginEnv
		case inFile[s.Key]:
			e.Origin = OriginFile
		}
		entries = append(entries, e)
	}
	return cfg, entries, nil
}

func collectKeys(prefix string, m map[string]any, out map[string]bool) {
	for k, v := range m {
		key := prefix + k
		if sub, ok := v.(map[string]any); ok {
			collectKeys(key+".", sub, out)
			continue
		}
		out[key] = true
	}
}

// SetFile changes one setting in the YAML file at path, keeping comments and
// the rest of the file as they are, and returns the canonical value written.
// The new file is checked as a whole before anything is written, and it is
// written in place so the owner and mode of the file stay.
func SetFile(path, key, raw string) (string, error) {
	value, err := Normalize(key, raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read config file: %w", err)
	}
	updated, err := editYAML(string(old), key, value)
	if err != nil {
		return "", err
	}

	check := Default()
	dec := yaml.NewDecoder(strings.NewReader(updated))
	dec.KnownFields(true)
	if err := dec.Decode(&check); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("the edit would leave %s unreadable: %w", path, err)
	}
	if got, _ := check.Get(key); got != value {
		return "", fmt.Errorf("cannot edit %s in %s automatically (it is written in a form this tool does not change); edit the file by hand", key, path)
	}
	if err := check.Validate(); err != nil {
		return "", fmt.Errorf("invalid configuration: %w", err)
	}

	if updated == string(old) {
		return value, nil
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return "", fmt.Errorf("write config file: %w (the file belongs to another user: run it with sudo)", err)
		}
		return "", fmt.Errorf("write config file: %w", err)
	}
	return value, nil
}

var commentRE = regexp.MustCompile(`^(\s*[A-Za-z_]+:)[ \t]*("[^"]*"|'[^']*'|[^#\s][^#]*?)?([ \t]+#.*)?[ \t]*$`)

// editYAML sets key (a dotted path of at most two names) in the text of a
// YAML file by changing the one line, so comments and layout survive.
func editYAML(text, key, value string) (string, error) {
	setting, _ := Lookup(key)
	rendered := value
	if setting.quote {
		rendered = strconv.Quote(value)
	}
	if rendered == "" {
		rendered = `""`
	}

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	parent, name, nested := strings.Cut(key, ".")
	if !nested {
		name, parent = parent, ""
	}

	// Find the line to change within the right block.
	start, end := 0, len(lines)
	if parent != "" {
		idx := -1
		for i, l := range lines {
			if regexp.MustCompile(`^` + regexp.QuoteMeta(parent) + `:[ \t]*(#.*)?$`).MatchString(l) {
				idx = i
				break
			}
		}
		if idx < 0 {
			lines = append(lines, parent+":", "  "+name+": "+rendered)
			return strings.Join(lines, "\n") + "\n", nil
		}
		start = idx + 1
		end = start
		for end < len(lines) && (lines[end] == "" || strings.HasPrefix(lines[end], " ") || strings.HasPrefix(lines[end], "\t")) {
			end++
		}
		// Trailing blank lines belong to what follows.
		for end > start && lines[end-1] == "" {
			end--
		}
	}
	keyRE := regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(name) + `:(\s|$)`)
	commented := regexp.MustCompile(`^(\s*)#[ \t]*` + regexp.QuoteMeta(name) + `:(\s|$)`)
	for i := start; i < end; i++ {
		l := lines[i]
		if parent == "" && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) {
			continue
		}
		if keyRE.MatchString(l) {
			m := commentRE.FindStringSubmatch(l)
			if m == nil {
				return "", fmt.Errorf("cannot edit %s automatically (unusual line %q); edit the file by hand", key, l)
			}
			lines[i] = m[1] + " " + rendered + m[3]
			return strings.Join(lines, "\n") + "\n", nil
		}
	}
	// An example left in a comment, such as `# agent_listen: ":8443"`, is
	// replaced by the real setting.
	for i := start; i < end; i++ {
		if m := commented.FindStringSubmatch(lines[i]); m != nil && (parent != "" || !strings.HasPrefix(lines[i], " ")) {
			indent := ""
			if parent != "" {
				indent = "  "
			}
			lines[i] = indent + name + ": " + rendered
			return strings.Join(lines, "\n") + "\n", nil
		}
	}
	// Not there: add it at the end of its block.
	line := name + ": " + rendered
	if parent != "" {
		line = "  " + line
	}
	lines = append(lines[:end], append([]string{line}, lines[end:]...)...)
	return strings.Join(lines, "\n") + "\n", nil
}
