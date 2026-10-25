package console

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/CodexCircle/Regulus/internal/config"
)

// Format is how a command prints.
type Format int

const (
	// FormatText is for people: aligned, with headings.
	FormatText Format = iota
	// FormatPlain is stable, one fact per line (key=value), for grep and awk.
	FormatPlain
	// FormatJSON is for programs.
	FormatJSON
)

// WriteStatus prints the snapshot.
func WriteStatus(w io.Writer, snap Snapshot, f Format, now time.Time) error {
	switch f {
	case FormatJSON:
		return writeJSON(w, snap)
	case FormatPlain:
		return writeStatusPlain(w, snap, now)
	}
	return writeStatusText(w, snap, now)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func state(snap Snapshot) string {
	if snap.Running {
		return T.Running
	}
	return T.Stopped
}

func writeStatusText(w io.Writer, snap Snapshot, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(k, v string) { _, _ = fmt.Fprintf(tw, "%s\t%s\n", k, v) }

	hub := state(snap)
	if snap.Running && snap.Uptime != "" {
		hub += ", " + T.Uptime + " " + snap.Uptime
	}
	row(T.Hub, hub+" ("+T.Service+": "+snap.Service+")")
	row(T.Version, snap.Version)
	row(T.Dashboard, snap.URL)
	for _, u := range snap.LANURLs {
		row("", T.AlsoOn+" "+u)
	}
	row(T.AgentPort, orDash(snap.AgentListen, T.Off))
	row(T.BuiltinAgent, onOff(snap.BuiltinAgent))
	data := snap.DataDir
	if snap.DatabaseSize > 0 {
		data += " (" + T.Database + " " + FormatBytes(snap.DatabaseSize) + ")"
	}
	row(T.DataDir, data)
	row(T.ConfigFile, orDash(snap.ConfigFile, T.None))
	if snap.Problem != "" {
		row("Problem", snap.Problem)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if snap.SetupToken != "" {
		_, _ = fmt.Fprintf(w, "\n%s: %s\n  %s\n", T.SetupTitle, T.SetupText, snap.SetupToken)
	}
	if snap.Running {
		_, _ = fmt.Fprintf(w, "\n%s (%d)\n", T.Agents, len(snap.Agents))
		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, a := range snap.Agents {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", a.ID, a.Status, heartbeat(a, now), a.Hostname)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintln(w)
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range snap.Health {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Status, c.Name, c.Detail)
	}
	return tw.Flush()
}

func writeStatusPlain(w io.Writer, snap Snapshot, now time.Time) error {
	p := func(k string, v any) { _, _ = fmt.Fprintf(w, "%s=%v\n", k, v) }
	p("version", snap.Version)
	p("service", snap.Service)
	p("running", snap.Running)
	p("listen", snap.Listen)
	p("port", snap.Port)
	p("url", snap.URL)
	for _, u := range snap.LANURLs {
		p("lan_url", u)
	}
	p("agent_listen", snap.AgentListen)
	p("data_dir", snap.DataDir)
	p("database_bytes", snap.DatabaseSize)
	p("builtin_agent", snap.BuiltinAgent)
	p("config_file", snap.ConfigFile)
	if snap.Problem != "" {
		p("problem", snap.Problem)
	}
	if snap.Running {
		p("uptime", snap.Uptime)
	}
	if snap.SetupToken != "" {
		p("setup_token", snap.SetupToken)
	}
	p("agents", len(snap.Agents))
	for _, a := range snap.Agents {
		p("agent."+a.ID, a.Status+" "+heartbeat(a, now))
	}
	for _, c := range snap.Health {
		p("check."+strings.ToLower(strings.ReplaceAll(c.Name, " ", "_")), c.Status+" "+c.Detail)
	}
	return nil
}

// WriteSettings prints the settings with their values and sources.
func WriteSettings(w io.Writer, entries []config.Entry, f Format) error {
	switch f {
	case FormatJSON:
		type item struct {
			Key         string   `json:"key"`
			Value       string   `json:"value"`
			Source      string   `json:"source"`
			Default     string   `json:"default"`
			Description string   `json:"description"`
			Choices     []string `json:"choices,omitempty"`
			Restart     bool     `json:"restart_required"`
		}
		def := config.Default()
		out := make([]item, 0, len(entries))
		for _, e := range entries {
			d, _ := def.Get(e.Key)
			out = append(out, item{e.Key, e.Value, e.Origin, d, e.Description, e.Choices, e.Restart})
		}
		return writeJSON(w, out)
	case FormatPlain:
		for _, e := range entries {
			_, _ = fmt.Fprintf(w, "%s=%s\n", e.Key, e.Value)
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", strings.ToUpper(T.SettingHeader[0]), strings.ToUpper(T.SettingHeader[1]), strings.ToUpper(T.SettingHeader[2]))
	for _, e := range entries {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Key, displayValue(e.Value), e.Origin)
	}
	return tw.Flush()
}

func displayValue(v string) string {
	if v == "" {
		return "(empty)"
	}
	return v
}

func orDash(v, empty string) string {
	if v == "" {
		return empty
	}
	return v
}

func onOff(b bool) string {
	if b {
		return T.On
	}
	return T.Off
}

// heartbeat describes when an agent was last heard from.
func heartbeat(a Agent, now time.Time) string {
	if a.LastHeartbeat.IsZero() {
		return "-"
	}
	return formatDuration(now.Sub(a.LastHeartbeat)) + " ago"
}
