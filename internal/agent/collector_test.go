package agent

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const statSample = `cpu  100 10 50 800 40 5 5 0 0 0
cpu0 50 5 25 400 20 2 3 0 0 0
intr 12345
`

const meminfoSample = `MemTotal:       16000000 kB
MemFree:         2000000 kB
MemAvailable:    6000000 kB
Buffers:          500000 kB
Cached:          3000000 kB
SwapTotal:       2000000 kB
SwapFree:        1500000 kB
HugePages_Total:       0
`

const mountsSample = `sysfs /sys sysfs rw,nosuid 0 0
proc /proc proc rw 0 0
/dev/sda2 / ext4 rw,relatime 0 0
tmpfs /run tmpfs rw 0 0
/dev/sda2 /var/lib/docker/bind ext4 rw 0 0
/dev/sdb1 /mnt/my\040disk xfs rw 0 0
/dev/sda1 /boot/efi vfat rw 0 0
overlay /var/lib/docker/overlay2/x overlay rw 0 0
`

const netDevSample = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000 10 0 0 0 0 0 0 1000 10 0 0 0 0 0 0
  eth0: 123456 100 0 0 0 0 0 0 654321 90 0 0 0 0 0 0
docker0: 5 1 0 0 0 0 0 0 7 1 0 0 0 0 0 0
`

func TestParseCPUTimes(t *testing.T) {
	got, err := parseCPUTimes(statSample)
	if err != nil {
		t.Fatal(err)
	}
	// total = 100+10+50+800+40+5+5 = 1010, idle = 800+40 = 840
	if got.total != 1010 || got.busy != 170 {
		t.Fatalf("got %+v, want busy 170 and total 1010", got)
	}
	for name, in := range map[string]string{"empty": "", "no cpu line": "intr 1\n", "garbage": "cpu a b c d e\n"} {
		if _, err := parseCPUTimes(in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCPUUsagePercent(t *testing.T) {
	tests := []struct {
		name      string
		prev, now cpuTimes
		want      float64
	}{
		{"half busy", cpuTimes{busy: 100, total: 200}, cpuTimes{busy: 150, total: 300}, 50},
		{"idle", cpuTimes{busy: 100, total: 200}, cpuTimes{busy: 100, total: 300}, 0},
		{"fully busy", cpuTimes{busy: 0, total: 0}, cpuTimes{busy: 100, total: 100}, 100},
		{"no time elapsed", cpuTimes{busy: 10, total: 100}, cpuTimes{busy: 10, total: 100}, 0},
		{"counters went backwards", cpuTimes{busy: 10, total: 100}, cpuTimes{busy: 5, total: 50}, 0},
	}
	for _, tt := range tests {
		if got := cpuUsagePercent(tt.prev, tt.now); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseLoadAvg(t *testing.T) {
	l1, l5, l15 := parseLoadAvg("0.52 0.40 0.31 1/234 5678\n")
	if l1 != 0.52 || l5 != 0.40 || l15 != 0.31 {
		t.Fatalf("got %v %v %v", l1, l5, l15)
	}
	if l1, l5, l15 = parseLoadAvg(""); l1 != 0 || l5 != 0 || l15 != 0 {
		t.Fatal("empty input should give zeros")
	}
}

func TestParseMemInfo(t *testing.T) {
	m, err := parseMemInfo(meminfoSample)
	if err != nil {
		t.Fatal(err)
	}
	const kb = 1024
	if m.TotalBytes != 16000000*kb || m.AvailableBytes != 6000000*kb || m.UsedBytes != 10000000*kb {
		t.Errorf("memory: %+v", m)
	}
	if m.SwapTotalBytes != 2000000*kb || m.SwapUsedBytes != 500000*kb {
		t.Errorf("swap: %+v", m)
	}

	// Kernels without MemAvailable fall back to free + buffers + cached.
	old, err := parseMemInfo("MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 50 kB\nCached: 150 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if old.AvailableBytes != 300*kb || old.UsedBytes != 700*kb {
		t.Errorf("fallback: %+v", old)
	}

	if _, err := parseMemInfo("SwapTotal: 1 kB\n"); err == nil {
		t.Error("expected an error without MemTotal")
	}
}

func TestParseMounts(t *testing.T) {
	got := parseMounts(mountsSample)
	want := []mount{
		{"/dev/sda2", "/", "ext4"},
		{"/dev/sdb1", "/mnt/my disk", "xfs"},
		{"/dev/sda1", "/boot/efi", "vfat"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mount %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseNetDev(t *testing.T) {
	got := parseNetDev(netDevSample)
	if len(got) != 2 {
		t.Fatalf("got %d interfaces, want 2 (loopback excluded): %+v", len(got), got)
	}
	if got[0].Interface != "eth0" || got[0].RxBytes != 123456 || got[0].TxBytes != 654321 {
		t.Errorf("eth0: %+v", got[0])
	}
	if got[1].Interface != "docker0" || got[1].RxBytes != 5 || got[1].TxBytes != 7 {
		t.Errorf("docker0: %+v", got[1])
	}
}

func writeProc(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLinuxCollectorFromFixtures(t *testing.T) {
	proc := t.TempDir()
	writeProc(t, proc, map[string]string{
		"stat":    statSample,
		"loadavg": "1.50 1.00 0.50 1/100 1\n",
		"meminfo": meminfoSample,
		"mounts":  mountsSample,
		"net/dev": netDevSample,
		"uptime":  "3600.50 7000.00\n",
	})
	statfs := func(path string) (uint64, uint64, error) {
		switch path {
		case "/":
			return 100, 40, nil
		case "/mnt/my disk":
			return 0, 0, nil // empty filesystems are skipped
		}
		return 0, 0, errors.New("not mounted")
	}
	c := newLinuxCollector(proc, statfs)

	// 200 more busy and 200 more idle jiffies since the collector was created.
	writeProc(t, proc, map[string]string{"stat": "cpu  300 10 50 1000 40 5 5 0 0 0\n"})
	r, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.GetCollectedAt() == nil || r.GetUptimeSeconds() != 3600 {
		t.Errorf("time and uptime: %+v", r)
	}
	if got := r.GetCpu().GetUsagePercent(); math.Abs(got-50) > 1e-9 {
		t.Errorf("cpu usage = %v, want 50", got)
	}
	if r.GetCpu().GetLoad_1() != 1.5 || r.GetCpu().GetLoad_15() != 0.5 {
		t.Errorf("load: %+v", r.GetCpu())
	}
	if r.GetMemory().GetTotalBytes() == 0 || r.GetMemory().GetUsedBytes() == 0 {
		t.Errorf("memory: %+v", r.GetMemory())
	}
	if len(r.GetDisks()) != 1 || r.GetDisks()[0].GetMountPoint() != "/" || r.GetDisks()[0].GetUsedBytes() != 40 {
		t.Errorf("disks: %+v", r.GetDisks())
	}
	if len(r.GetNetworks()) != 2 {
		t.Errorf("networks: %+v", r.GetNetworks())
	}

	// The next report covers only the time since the previous one.
	writeProc(t, proc, map[string]string{"stat": "cpu  300 10 50 1100 40 5 5 0 0 0\n"})
	r, err = c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := r.GetCpu().GetUsagePercent(); got != 0 {
		t.Errorf("cpu usage after an idle interval = %v, want 0", got)
	}
}

func TestLinuxCollectorWithoutProcFails(t *testing.T) {
	c := newLinuxCollector(t.TempDir(), func(string) (uint64, uint64, error) { return 0, 0, errors.New("no") })
	if _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected an error when neither CPU nor memory can be read")
	}
}

func TestLinuxCollectorOnThisMachine(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	if _, err := parseCPUTimes(readFile("/proc/stat")); err != nil {
		t.Skip("/proc/stat is not readable")
	}
	r, err := NewLinuxCollector().Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cpu := r.GetCpu().GetUsagePercent(); cpu < 0 || cpu > 100 {
		t.Errorf("cpu usage %v out of range", cpu)
	}
	m := r.GetMemory()
	if m.GetTotalBytes() == 0 || m.GetUsedBytes() > m.GetTotalBytes() {
		t.Errorf("implausible memory: %+v", m)
	}
	if r.GetUptimeSeconds() == 0 {
		t.Error("uptime is zero")
	}
}

func TestDefaultCollectorFallsBackWithoutProc(t *testing.T) {
	// On a machine with /proc the default is the Linux collector.
	if _, err := parseCPUTimes(readFile("/proc/stat")); err != nil {
		if _, ok := defaultCollector().(basicCollector); !ok {
			t.Fatal("expected the basic collector without /proc/stat")
		}
		return
	}
	if _, ok := defaultCollector().(*LinuxCollector); !ok {
		t.Fatal("expected the Linux collector")
	}
}
