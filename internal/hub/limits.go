package hub

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

// What one agent can make the hub receive, keep and broadcast. An agent is a
// remote machine, so anything it sends is untrusted: a compromised or buggy
// one must not be able to exhaust the memory of the hub.
const (
	// maxAgentMessageBytes is the largest message the agent port accepts. A
	// Hello or a metrics report with the lists below is a few kilobytes, and
	// an enrollment request carries one certificate request.
	maxAgentMessageBytes = 256 << 10
	// maxAgentStreams bounds the streams of one connection; an agent uses one.
	maxAgentStreams = 8
	// maxListEntries bounds the disks and the network interfaces of a report.
	maxListEntries = 64
	// maxTextBytes bounds every text an agent reports (host name, version,
	// mount point, interface name…).
	maxTextBytes = 256
	// minReportGapFraction: a report that comes sooner than this fraction of the
	// metrics interval after the previous one is dropped.
	minReportGapFraction = 2
)

// cleanText cuts s to maxTextBytes (on a character boundary) and removes
// control characters, so that nothing an agent says can disturb logs, the
// terminal console or the dashboard.
func cleanText(s string) string {
	if len(s) > maxTextBytes {
		s = s[:maxTextBytes]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	if strings.IndexFunc(s, unicode.IsControl) < 0 && utf8.ValidString(s) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
}

// sanitizeHello limits the texts of Hello in place.
func sanitizeHello(h *agentv1.Hello) {
	h.AgentVersion = cleanText(h.GetAgentVersion())
	h.AgentId = cleanText(h.GetAgentId())
	if sys := h.GetSystem(); sys != nil {
		sys.Hostname = cleanText(sys.GetHostname())
		sys.Os = cleanText(sys.GetOs())
		sys.OsVersion = cleanText(sys.GetOsVersion())
		sys.Kernel = cleanText(sys.GetKernel())
		sys.Arch = cleanText(sys.GetArch())
	}
}

// sanitizeReport limits the lists and the texts of a report in place and
// reports whether it had to cut something.
func sanitizeReport(r *agentv1.MetricsReport) (cut bool) {
	if len(r.Disks) > maxListEntries {
		r.Disks, cut = r.Disks[:maxListEntries], true
	}
	if len(r.Networks) > maxListEntries {
		r.Networks, cut = r.Networks[:maxListEntries], true
	}
	for _, d := range r.Disks {
		d.MountPoint, d.Filesystem = cleanText(d.GetMountPoint()), cleanText(d.GetFilesystem())
	}
	for _, n := range r.Networks {
		n.Interface = cleanText(n.GetInterface())
	}
	return cut
}

// reportTooSoon tells whether a report arrived before the allowed gap since
// the previous one. The agent sends one every metricsInterval, so half of it
// leaves room for jitter and for the report sent right after a reconnect.
func reportTooSoon(last, now time.Time, interval time.Duration) bool {
	if last.IsZero() || interval <= 0 {
		return false
	}
	return now.Sub(last) < interval/minReportGapFraction
}
