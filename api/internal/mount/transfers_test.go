package mount

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitTransfer(t *testing.T, s *Service, id, state string) TransferTask {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		for _, task := range s.Transfers() {
			if task.ID == id {
				if task.State == state {
					return task
				}
				if task.State == "failed" && state != "failed" {
					t.Fatalf("transfer failed: %s", task.Error)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task did not reach %s: %+v", state, s.Transfers())
	return TransferTask{}
}
func TestTransferNestedFoldersConflictAndPersistence(t *testing.T) {
	s, root, _ := setup(t)
	ctx := context.Background()
	from := filepath.Join(root, "source")
	to := filepath.Join(root, "target")
	os.MkdirAll(filepath.Join(from, "album", "empty"), 0700)
	os.MkdirAll(to, 0700)
	os.WriteFile(filepath.Join(from, "album", "photo.txt"), []byte("nested content"), 0600)
	os.WriteFile(filepath.Join(from, "other.txt"), []byte("other"), 0600)
	source, err := s.Create(ctx, Input{Name: "source", Provider: "nas", Config: Config{Root: from, CacheMinutes: 30}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.Create(ctx, Input{Name: "target", Provider: "nas", Config: Config{Root: to, CacheMinutes: 30}})
	if err != nil {
		t.Fatal(err)
	}
	input := TransferInput{SourceAccount: source.ID, IDs: []string{"album", "other.txt"}, TargetAccount: target.ID, Conflict: "skip"}
	task, err := s.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitTransfer(t, s, task.ID, "completed")
	if finished.TotalFiles != 2 || finished.CompletedFiles != 2 || finished.ProcessedBytes != finished.TotalBytes {
		t.Fatalf("bad totals %+v", finished)
	}
	content, err := os.ReadFile(filepath.Join(to, "album", "photo.txt"))
	if err != nil || string(content) != "nested content" {
		t.Fatal("missing nested content", err)
	}
	if info, err := os.Stat(filepath.Join(to, "album", "empty")); err != nil || !info.IsDir() {
		t.Fatal("empty directory lost")
	}
	if _, err = os.Stat(filepath.Join(from, "other.txt")); err != nil {
		t.Fatal("source was removed")
	}
	repeat, err := s.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	skipped := waitTransfer(t, s, repeat.ID, "completed")
	if skipped.SkippedFiles != 2 {
		t.Fatal("conflicts did not skip", skipped)
	}
	input.Conflict = "rename"
	renamed, err := s.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, s, renamed.ID, "completed")
	if _, err = os.Stat(filepath.Join(to, "album", "photo (1).txt")); err != nil {
		t.Fatal("rename conflict failed", err)
	}
	var payload string
	if err = s.store.db.QueryRow(`SELECT payload FROM transfer_tasks WHERE id=?`, task.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var record transferRecord
	if json.Unmarshal([]byte(payload), &record) != nil || record.Task.State != "completed" {
		t.Fatal("task not persisted")
	}
	if _, err = s.TransferAction(task.ID, "remove"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(to, "other.txt")); err != nil {
		t.Fatal("removing task record removed downloaded files")
	}
	if _, err = s.CreateTransfer(ctx, TransferInput{SourceAccount: source.ID, IDs: []string{"album"}, TargetAccount: source.ID, TargetParent: "album", Conflict: "skip"}); err == nil {
		t.Fatal("recursive transfer accepted")
	}
}

type cancelBody struct{ ctx context.Context }

func (b cancelBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (b cancelBody) Close() error             { return nil }
func TestQuarkTransferCancelPauseRetryAndNoCredentialPersistence(t *testing.T) {
	s, root, _ := setup(t)
	ctx := context.Background()
	target, err := s.Create(ctx, Input{Name: "disk", Provider: "nas", Config: Config{Root: root}})
	if err != nil {
		t.Fatal(err)
	}
	source := Account{ID: ID(), Name: "quark", Provider: "quark", Config: Config{Root: "0", CacheMinutes: 30}}
	if err = s.store.Put(ctx, source, Credentials{Cookie: "private-cookie-not-in-task"}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 8)
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/1/clouddrive/file/sort":
			return response(`{"status":200,"data":{"list":[{"fid":"file","file_name":"remote.txt","file_type":1,"size":12}]}}`), nil
		case "/1/clouddrive/file/download":
			return response(`{"status":200,"data":[{"download_url":"https://cdn.quark.cn/content"}]}`), nil
		case "/content":
			if r.Header.Get("Cookie") != "private-cookie-not-in-task" {
				t.Error("download missing credential")
			}
			started <- struct{}{}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: cancelBody{r.Context()}}, nil
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			return response(`{}`), nil
		}
	})
	task, err := s.CreateTransfer(ctx, TransferInput{SourceAccount: source.ID, IDs: []string{"file"}, TargetAccount: target.ID, Conflict: "skip"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}
	if _, err = s.TransferAction(task.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.transfers.mu.Lock()
		_, running := s.transfers.running[task.ID]
		s.transfers.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitTransfer(t, s, task.ID, "paused")
	if matches, _ := filepath.Glob(filepath.Join(root, ".cloud-part-*")); len(matches) > 0 {
		t.Fatal("partial files retained after pause")
	}
	if _, err = s.TransferAction(task.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("retry never started")
	}
	if _, err = s.TransferAction(task.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, s, task.ID, "canceled")
	var payload string
	s.store.db.QueryRow(`SELECT payload FROM transfer_tasks WHERE id=?`, task.ID).Scan(&payload)
	if strings.Contains(payload, "private-cookie") {
		t.Fatal("credential persisted in task")
	}
	if _, err = s.CreateTransfer(ctx, TransferInput{SourceAccount: source.ID, IDs: []string{"outside-root"}, TargetAccount: target.ID, Conflict: "skip"}); err == nil {
		t.Fatal("unlisted source accepted")
	}
}

func TestInterruptedTaskRestoresPaused(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "files")
	os.Mkdir(root, 0700)
	store, err := OpenStore(filepath.Join(directory, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TABLE transfer_tasks(id TEXT PRIMARY KEY,payload TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	record := transferRecord{Task: TransferTask{ID: "interrupted", State: "running", Name: "retained"}, Done: map[string]transferResult{}}
	payload, _ := json.Marshal(record)
	store.db.Exec(`INSERT INTO transfer_tasks VALUES(?,?)`, record.Task.ID, string(payload))
	s, err := New(store, []string{root}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tasks := s.Transfers()
	if len(tasks) != 1 || tasks[0].State != "paused" || tasks[0].Error == "" {
		t.Fatal("interrupted task not safely restored", tasks)
	}
}

func TestConcurrentQuarkDownloadsKeepBothNames(t *testing.T) {
	s, root, _ := setup(t)
	ctx := context.Background()
	target, err := s.Create(ctx, Input{Name: "disk", Provider: "nas", Config: Config{Root: root}})
	if err != nil {
		t.Fatal(err)
	}
	source := Account{ID: ID(), Name: "source", Provider: "quark", Config: Config{Root: "0", CacheMinutes: 30}}
	if err = s.store.Put(ctx, source, Credentials{Cookie: "private"}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/1/clouddrive/file/sort":
			return response(`{"status":200,"data":{"list":[{"fid":"file","file_name":"remote.txt","file_type":1,"size":3}]}}`), nil
		case "/1/clouddrive/file/download":
			return response(`{"status":200,"data":[{"download_url":"https://cdn.quark.cn/content"}]}`), nil
		case "/content":
			started <- struct{}{}
			select {
			case <-release:
				return response("abc"), nil
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		default:
			t.Errorf("unexpected URL %s", r.URL.Path)
			return response(`{}`), nil
		}
	})
	input := TransferInput{SourceAccount: source.ID, IDs: []string{"file"}, TargetAccount: target.ID, Conflict: "rename"}
	first, err := s.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("two workers did not start")
		}
	}
	close(release)
	released = true
	waitTransfer(t, s, first.ID, "completed")
	waitTransfer(t, s, second.ID, "completed")
	for _, name := range []string{"remote.txt", "remote (1).txt"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != "abc" {
			t.Fatalf("concurrent publication lost %s: %v", name, err)
		}
	}
}
