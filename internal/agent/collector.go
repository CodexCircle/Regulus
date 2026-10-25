package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
)

// realFilesystems are the filesystem types reported as disks. Pseudo
// filesystems (proc, tmpfs, overlay, cgroup, ...) are left out on purpose.
var realFilesystems = []string{
	"ext2", "ext3", "ext4", "xfs", "btrfs", "zfs", "f2fs", "jfs", "reiserfs",
	"vfat", "exfat", "ntfs", "ntfs3", "fuseblk",
}

// statfsFunc returns the total and used bytes of the filesystem mounted at path.
type statfsFunc func(path string) (total, used uint64, err error)

// LinuxCollector reads CPU, memory, disk, network and uptime metrics from
// /proc and statfs. CPU usage is computed between two consecutive calls, so
// the first report covers the time since the collector was created.
type LinuxCollector struct {
	proc   string
	statfs statfsFunc

	mu      sync.Mutex
	prevCPU cpuTimes
}

// NewLinuxCollector returns a collector that reads /proc.
func NewLinuxCollector() *LinuxCollector {
	return newLinuxCollector("/proc", diskUsage)
}

func newLinuxCollector(proc string, statfs statfsFunc) *LinuxCollector {
	c := &LinuxCollector{proc: proc, statfs: statfs}
	if t, err := parseCPUTimes(c.read("stat")); err == nil {
		c.prevCPU = t
	}
	return c
}

func (c *LinuxCollector) read(name string) string {
	data, err := os.ReadFile(filepath.Join(c.proc, name))
	if err != nil {
		return ""
	}
	return string(data)
}

// Collect gathers a report. A source that cannot be read leaves its part of the
// report empty; it fails only when neither the CPU nor the memory can be read
// (for example on a system without /proc).
func (c *LinuxCollector) Collect(context.Context) (*agentv1.MetricsReport, error) {
	now, err := parseCPUTimes(c.read("stat"))
	var usage float64
	c.mu.Lock()
	if err == nil {
		usage = cpuUsagePercent(c.prevCPU, now)
		c.prevCPU = now
	}
	c.mu.Unlock()

	mem, memErr := parseMemInfo(c.read("meminfo"))
	if err != nil && memErr != nil {
		return nil, fmt.Errorf("cannot read /proc: %w", errors.Join(err, memErr))
	}

	report := &agentv1.MetricsReport{
		CollectedAt:   timestamppb.New(time.Now()),
		Cpu:           &agentv1.CpuMetrics{UsagePercent: usage},
		Memory:        mem,
		Disks:         c.disks(),
		Networks:      parseNetDev(c.read("net/dev")),
		UptimeSeconds: parseUptime(c.read("uptime")),
	}
	report.Cpu.Load_1, report.Cpu.Load_5, report.Cpu.Load_15 = parseLoadAvg(c.read("loadavg"))
	return report, nil
}

func (c *LinuxCollector) disks() []*agentv1.DiskMetrics {
	var disks []*agentv1.DiskMetrics
	for _, m := range parseMounts(c.read("mounts")) {
		total, used, err := c.statfs(m.mountPoint)
		if err != nil || total == 0 {
			continue
		}
		disks = append(disks, &agentv1.DiskMetrics{
			MountPoint: m.mountPoint, Filesystem: m.fsType, TotalBytes: total, UsedBytes: used,
		})
	}
	return disks
}

// cpuTimes are the cumulative jiffies of the aggregate "cpu" line of /proc/stat.
type cpuTimes struct {
	busy, total uint64
}

// parseCPUTimes reads the aggregate "cpu" line of /proc/stat. Idle time
// includes iowait; guest time is already counted in user and nice.
func parseCPUTimes(content string) (cpuTimes, error) {
	for line := range strings.SplitSeq(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var v [8]uint64 // user nice system idle iowait irq softirq steal
		for i := range v {
			if i+1 >= len(fields) {
				break
			}
			n, err := strconv.ParseUint(fields[i+1], 10, 64)
			if err != nil {
				return cpuTimes{}, fmt.Errorf("invalid cpu line %q: %w", line, err)
			}
			v[i] = n
		}
		var total uint64
		for _, n := range v {
			total += n
		}
		idle := v[3] + v[4]
		return cpuTimes{busy: total - idle, total: total}, nil
	}
	return cpuTimes{}, errors.New("no cpu line in /proc/stat")
}

// cpuUsagePercent is the share of busy time between two samples, 0 to 100.
func cpuUsagePercent(prev, now cpuTimes) float64 {
	if now.total <= prev.total || now.busy < prev.busy {
		return 0
	}
	pct := 100 * float64(now.busy-prev.busy) / float64(now.total-prev.total)
	return min(max(pct, 0), 100)
}

// parseLoadAvg returns the 1, 5 and 15 minute load averages from /proc/loadavg.
func parseLoadAvg(content string) (l1, l5, l15 float64) {
	fields := strings.Fields(content)
	if len(fields) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(fields[0], 64)
	l5, _ = strconv.ParseFloat(fields[1], 64)
	l15, _ = strconv.ParseFloat(fields[2], 64)
	return l1, l5, l15
}

// parseMemInfo reads /proc/meminfo. Used memory is total minus available.
func parseMemInfo(content string) (*agentv1.MemoryMetrics, error) {
	values := map[string]uint64{}
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && fields[1] == "kB" {
			n *= 1024
		}
		values[name] = n
	}
	total, ok := values["MemTotal"]
	if !ok {
		return nil, errors.New("no MemTotal in /proc/meminfo")
	}
	available, ok := values["MemAvailable"]
	if !ok { // kernels before 3.14
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	available = min(available, total)
	swapTotal := values["SwapTotal"]
	swapFree := min(values["SwapFree"], swapTotal)
	return &agentv1.MemoryMetrics{
		TotalBytes:     total,
		UsedBytes:      total - available,
		AvailableBytes: available,
		SwapTotalBytes: swapTotal,
		SwapUsedBytes:  swapTotal - swapFree,
	}, nil
}

type mount struct {
	device, mountPoint, fsType string
}

// parseMounts returns the mounts of real filesystems from /proc/mounts, one
// per device (bind mounts and subvolumes of the same device are reported once,
// at the shortest mount point).
func parseMounts(content string) []mount {
	var mounts []mount
	seen := map[string]int{} // device -> index in mounts
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || !slices.Contains(realFilesystems, fields[2]) {
			continue
		}
		m := mount{device: fields[0], mountPoint: unescapeMount(fields[1]), fsType: fields[2]}
		if i, ok := seen[m.device]; ok {
			if len(m.mountPoint) < len(mounts[i].mountPoint) {
				mounts[i] = m
			}
			continue
		}
		seen[m.device] = len(mounts)
		mounts = append(mounts, m)
	}
	return mounts
}

// unescapeMount decodes the octal escapes /proc/mounts uses for spaces and
// other special characters in paths.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseNetDev returns the received and transmitted byte counters of every
// interface except loopback, from /proc/net/dev.
func parseNetDev(content string) []*agentv1.NetworkMetrics {
	var nets []*agentv1.NetworkMetrics
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok { // the two header lines have no colon
			continue
		}
		name = strings.TrimSpace(name)
		fields := strings.Fields(rest) // rx: bytes packets errs drop fifo frame compressed multicast; tx: bytes ...
		if name == "lo" || len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		nets = append(nets, &agentv1.NetworkMetrics{Interface: name, RxBytes: rx, TxBytes: tx})
	}
	return nets
}
