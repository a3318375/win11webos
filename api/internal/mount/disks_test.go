package mount

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDiskSnapshotSharedAndCopied(t *testing.T) {
	m := &DiskMonitor{checked: time.Now(), snapshot: []DiskStatus{{Path: "/data", State: "sleeping"}}}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items := m.Snapshot(context.Background())
			if items[0].State != "sleeping" {
				t.Error("cached state changed")
			}
			items[0].State = "active"
		}()
	}
	wg.Wait()
	if m.snapshot[0].State != "sleeping" {
		t.Fatal("consumer mutated shared cache")
	}
}
func TestDiskSnapshotColdAndExpired(t *testing.T) {
	m := &DiskMonitor{roots: []string{}}
	m.Snapshot(context.Background())
	if m.checked.IsZero() {
		t.Fatal("cold cache was not populated")
	}
	first := m.checked
	m.Snapshot(context.Background())
	if !m.checked.Equal(first) {
		t.Fatal("fresh cache sampled again")
	}
	m.checked = time.Now().Add(-6 * time.Minute)
	m.Snapshot(context.Background())
	if time.Since(m.checked) > time.Second {
		t.Fatal("expired cache was not sampled")
	}
}
