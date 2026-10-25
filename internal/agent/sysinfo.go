package agent

import (
	"bufio"
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

// systemInfo describes this machine for Hello. Fields that cannot be read
// (for example on a system without /proc) are left empty.
func systemInfo() *agentv1.SystemInfo {
	hostname, _ := os.Hostname()
	return &agentv1.SystemInfo{
		Hostname:         hostname,
		Os:               runtime.GOOS,
		OsVersion:        parseOSRelease(readFile("/etc/os-release")),
		Kernel:           strings.TrimSpace(readFile("/proc/sys/kernel/osrelease")),
		Arch:             runtime.GOARCH,
		CpuCount:         uint32(runtime.NumCPU()),
		MemoryTotalBytes: parseMemTotal(readFile("/proc/meminfo")),
	}
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// parseOSRelease returns PRETTY_NAME from the content of /etc/os-release.
func parseOSRelease(content string) string {
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := sc.Text()
		if value, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(value, `"'`)
		}
	}
	return ""
}

// parseMemTotal returns MemTotal, in bytes, from the content of /proc/meminfo.
func parseMemTotal(content string) uint64 {
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "MemTotal:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest) // for example ["16318464", "kB"]
		if len(fields) == 0 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// parseUptime returns the whole seconds in the content of /proc/uptime.
func parseUptime(content string) uint64 {
	fields := strings.Fields(content) // for example ["350735.47", "234388.90"]
	if len(fields) == 0 {
		return 0
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return uint64(seconds)
}

// Collector gathers the metrics sent to the hub.
type Collector interface {
	Collect(ctx context.Context) (*agentv1.MetricsReport, error)
}

// basicCollector reports only the time and the uptime. The full Linux
// collector (CPU, memory, disk, network) replaces it.
type basicCollector struct{}

func (basicCollector) Collect(context.Context) (*agentv1.MetricsReport, error) {
	return &agentv1.MetricsReport{
		CollectedAt:   timestamppb.New(time.Now()),
		UptimeSeconds: parseUptime(readFile("/proc/uptime")),
	}, nil
}
