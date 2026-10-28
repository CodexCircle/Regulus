package console

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/localstatus"
	"github.com/CodexCircle/Regulus/internal/storage"
)

// ServiceUnit is the systemd unit of the hub.
const ServiceUnit = "regulus"

// Check statuses.
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
	Info = "info"
)

// Check is one health check.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Agent is a connected agent as the hub reports it.
type Agent = localstatus.Agent

// Snapshot is everything the console shows about the hub at one moment.
type Snapshot struct {
	Version string `json:"version"`
	// ConfigFile is the file the settings are read from and written to, or
	// "" when there is none.
	ConfigFile string `json:"config_file"`
	// Problem is set when the configuration cannot be loaded.
	Problem string `json:"problem,omitempty"`

	// Service is the state of the systemd unit: active, inactive, failed,
	// or unknown when systemd does not know it or is not there.
	Service string `json:"service"`
	// Running is true when the hub answered on its local socket.
	Running bool `json:"running"`

	Listen      string   `json:"listen"`
	Port        string   `json:"port"`
	URL         string   `json:"url"`
	LANURLs     []string `json:"lan_urls,omitempty"`
	AgentListen string   `json:"agent_listen,omitempty"`
	// CAFingerprint is what a new agent pins, from the running hub.
	CAFingerprint string `json:"ca_fingerprint,omitempty"`
	DataDir       string `json:"data_dir"`
	DatabaseSize  int64  `json:"database_bytes"`
	BuiltinAgent  bool   `json:"builtin_agent"`
	// LogLevel and Format are shown in the header of the logs.
	LogLevel string `json:"log_level"`

	StartedAt  time.Time `json:"started_at,omitzero"`
	Uptime     string    `json:"uptime,omitempty"`
	SetupToken string    `json:"setup_token,omitempty"`
	Agents     []Agent   `json:"agents"`

	Settings []config.Entry `json:"-"`
	Health   []Check        `json:"health"`
}

// System is how the console looks at the machine. Tests replace it.
type System struct {
	Getenv func(string) string
	Now    func() time.Time
	// ServiceState returns the systemd state of the hub's unit.
	ServiceState func(ctx context.Context) string
	// Fetch asks the running hub for its status.
	Fetch func(ctx context.Context, dataDir string) (localstatus.Status, error)
	// Addrs lists the IPv4 addresses of this machine, besides loopback.
	Addrs func() []string
	// Logs returns the last lines of the hub's log.
	Logs func(ctx context.Context, n int) ([]string, error)
}

// HostSystem is the real machine.
func HostSystem() System {
	return System{
		Getenv:       os.Getenv,
		Now:          time.Now,
		ServiceState: systemdState,
		Fetch:        localstatus.Fetch,
		Addrs:        lanAddrs,
		Logs:         journalLogs,
	}
}

func systemdState(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", ServiceUnit).Output()
	state := strings.TrimSpace(string(out))
	if state == "" || (err != nil && state == "") {
		return "unknown"
	}
	return state
}

func journalLogs(ctx context.Context, n int) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "journalctl", "-u", ServiceUnit, "-n", fmt.Sprint(n), "--no-pager", "-o", "cat").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func lanAddrs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, ipn.IP.String())
	}
	return out
}

// Collect gathers the snapshot: the configuration (from configFile, the
// environment and the defaults), the state of the service and, when the hub
// runs, what it says about itself.
func Collect(ctx context.Context, sys System, configFile, version string) Snapshot {
	snap := Snapshot{Version: version, ConfigFile: configFile, Agents: []Agent{}}
	cfg, entries, err := config.Resolve(configFile, sys.Getenv)
	if err != nil {
		snap.Problem = err.Error()
		cfg = config.Default()
		_, entries, _ = config.Resolve("", func(string) string { return "" })
	}
	snap.Settings = entries
	snap.Listen = cfg.Listen
	snap.Port = portOf(cfg.Listen)
	snap.AgentListen = cfg.AgentListen
	snap.DataDir = cfg.DataDir
	snap.BuiltinAgent = cfg.BuiltinAgent
	snap.LogLevel = cfg.Log.Level
	snap.URL = dashboardURL(cfg.Listen)
	if hostOf(cfg.Listen) == "" || isWildcard(hostOf(cfg.Listen)) {
		for _, a := range sys.Addrs() {
			snap.LANURLs = append(snap.LANURLs, "http://"+net.JoinHostPort(a, snap.Port))
		}
	}
	if fi, err := os.Stat(filepath.Join(cfg.DataDir, storage.FileName)); err == nil {
		snap.DatabaseSize = fi.Size()
	}

	snap.Service = sys.ServiceState(ctx)
	live, fetchErr := sys.Fetch(ctx, cfg.DataDir)
	if fetchErr == nil {
		snap.Running = true
		snap.StartedAt = live.StartedAt
		snap.Uptime = formatDuration(sys.Now().Sub(live.StartedAt))
		snap.SetupToken = live.SetupToken
		snap.Agents = live.Agents
		if live.Version != "" {
			snap.Version = live.Version
		}
		if live.AgentAddr != "" {
			snap.AgentListen = live.AgentAddr
		}
		snap.CAFingerprint = live.CAFingerprint
	}
	snap.Health = healthChecks(snap, cfg, fetchErr)
	return snap
}

func healthChecks(snap Snapshot, cfg config.Config, fetchErr error) []Check {
	var checks []Check
	add := func(name, status, detail string) { checks = append(checks, Check{name, status, detail}) }

	switch {
	case snap.Problem != "":
		add("Configuration", Fail, snap.Problem)
	case snap.ConfigFile == "":
		add("Configuration", Warn, "no configuration file: showing the defaults; pass --config FILE to read and change one")
	default:
		add("Configuration", OK, snap.ConfigFile+" is valid")
	}

	switch {
	case snap.Running:
		add("Hub", OK, "answers on its local socket")
	case fetchErr != nil && errors.Is(fetchErr, fs.ErrPermission):
		add("Hub", Warn, "cannot read its local socket: run as the hub's user (for example sudo -u regulus regulus)")
	case snap.Service == "active":
		add("Hub", Warn, "the service is active but does not answer on "+localstatus.SocketPath(snap.DataDir))
	default:
		add("Hub", Warn, "not running")
	}

	st, detail := portCheck(cfg.Listen, snap.Running)
	add("Dashboard port", st, detail)
	if cfg.AgentListen != "" {
		st, detail = portCheck(cfg.AgentListen, snap.Running)
		add("Agent port", st, detail)
	}

	switch err := writable(snap.DataDir); {
	case errors.Is(err, fs.ErrNotExist):
		add("Data directory", Warn, snap.DataDir+" does not exist yet (created at the first start)")
	case err != nil:
		add("Data directory", Warn, "you cannot write to "+snap.DataDir+" (fine if the hub runs as another user)")
	default:
		add("Data directory", OK, snap.DataDir+" is writable")
	}

	if _, err := os.Stat(filepath.Join(snap.DataDir, storage.FileName)); err == nil {
		add("Database", OK, fmt.Sprintf("%s, %s", storage.FileName, FormatBytes(snap.DatabaseSize)))
	} else {
		add("Database", Info, "none yet")
	}
	return checks
}

// portCheck says whether the address can be used. While the hub runs it is
// the hub that holds it.
func portCheck(addr string, hubRunning bool) (string, string) {
	if hubRunning {
		return OK, addr + " is served by the hub"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return Fail, addr + " is in use by another program"
		}
		if errors.Is(err, fs.ErrPermission) {
			return Warn, addr + " needs more privileges than yours (ports below 1024)"
		}
		return Warn, addr + ": " + err.Error()
	}
	_ = ln.Close()
	return OK, addr + " is free"
}

func writable(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".regulus-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

func hostOf(listen string) string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	return host
}

func portOf(listen string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	return port
}

func isWildcard(host string) bool { return host == "0.0.0.0" || host == "::" }

// dashboardURL is where to open the dashboard on this machine.
func dashboardURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://localhost:8080"
	}
	if host == "" || isWildcard(host) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// FormatBytes writes a size for people: 1.5 MiB.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	s := int((d - time.Duration(m)*time.Minute) / time.Second)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, h, m)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
