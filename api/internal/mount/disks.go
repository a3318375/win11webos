package mount

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DiskMonitor owns the only hardware sampling path. Consumers receive copies.
// No directory scans, capacity queries, SMART scans or filesystem reads here.
type DiskStatus struct {
	Path      string    `json:"path"`
	Device    string    `json:"device"`
	State     string    `json:"state"`
	Reason    string    `json:"reason"`
	CheckedAt time.Time `json:"checkedAt"`
}
type DiskMonitor struct {
	mu       sync.Mutex
	roots    []string
	snapshot []DiskStatus
	checked  time.Time
	cancel   context.CancelFunc
}

func newDiskMonitor(roots []string) *DiskMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	m := &DiskMonitor{roots: append([]string(nil), roots...), cancel: cancel}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Snapshot(ctx)
			}
		}
	}()
	return m
}
func (m *DiskMonitor) Snapshot(ctx context.Context) []DiskStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.checked.IsZero() || time.Since(m.checked) >= 5*time.Minute {
		result := make([]DiskStatus, 0, len(m.roots))
		for _, root := range m.roots {
			result = append(result, probeDisk(ctx, root))
		}
		m.snapshot = result
		m.checked = time.Now()
	}
	return append([]DiskStatus(nil), m.snapshot...)
}
func probeDisk(ctx context.Context, root string) DiskStatus {
	d := DiskStatus{Path: root, State: "unknown", Reason: "无法安全确认硬盘状态，暂不可选", CheckedAt: time.Now()}
	if runtime.GOOS != "linux" {
		d.Reason = "当前系统不支持无唤醒硬盘探测"
		return d
	}
	// /proc and /sys expose kernel metadata; never traverse the mounted filesystem.
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return d
	}
	best := ""
	dev := ""
	for _, line := range strings.Split(string(data), "\n") {
		halves := strings.Split(line, " - ")
		if len(halves) != 2 {
			continue
		}
		f := strings.Fields(halves[0])
		tail := strings.Fields(halves[1])
		if len(f) < 5 || len(tail) < 2 {
			continue
		}
		mountpoint := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(f[4])
		if inside(mountpoint, root) && len(mountpoint) > len(best) {
			best = mountpoint
			dev = tail[1]
		}
	}
	if !strings.HasPrefix(dev, "/dev/") {
		return d
	}
	device := filepath.Base(dev)
	sys, err := filepath.EvalSymlinks("/sys/class/block/" + device)
	if err != nil {
		return d
	}
	if _, err := os.ReadFile(filepath.Join(sys, "partition")); err == nil {
		sys = filepath.Dir(sys)
		device = filepath.Base(sys)
	}
	d.Device = "/dev/" + device
	power, _ := os.ReadFile(filepath.Join(sys, "device/power/runtime_status"))
	if strings.TrimSpace(string(power)) == "suspended" {
		d.State = "sleeping"
		d.Reason = "硬盘已休眠"
		return d
	}
	rotational, err := os.ReadFile(filepath.Join(sys, "queue/rotational"))
	if err != nil {
		return d
	}
	if strings.TrimSpace(string(rotational)) == "0" && strings.HasPrefix(device, "nvme") {
		d.State = "active"
		d.Reason = "NVMe 存储可用"
		return d
	}
	// Only direct ATA devices: avoid USB/RAID bridges with unknown wake behavior.
	if !strings.Contains(sys, "/ata") || !strings.HasPrefix(device, "sd") {
		return d
	}
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(call, "hdparm", "-C", d.Device).Output()
	if err != nil {
		return d
	}
	text := strings.ToLower(string(output))
	if strings.Contains(text, "standby") || strings.Contains(text, "sleeping") {
		d.State = "sleeping"
		d.Reason = "硬盘已休眠"
	} else if strings.Contains(text, "active/idle") {
		d.State = "active"
		d.Reason = "硬盘处于活动状态"
	}
	return d
}

func (s *Service) Disks(ctx context.Context) []DiskStatus { return s.disks.Snapshot(ctx) }
