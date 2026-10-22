package mount

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalBatchActionsAndNoOverwrite(t *testing.T) {
	s, root, _ := setup(t)
	ctx := context.Background()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.Create(ctx, Input{Name: "操作测试", Provider: "nas", Config: Config{Root: root, CacheMinutes: 30}})
	if err != nil {
		t.Fatal(err)
	}
	run := func(in FileAction, want int) {
		t.Helper()
		n, e := s.FileActions(ctx, a.ID, in)
		if e != nil || n != want {
			t.Fatalf("%s: %d %v", in.Action, n, e)
		}
	}
	run(FileAction{Action: "mkdir", Name: "target"}, 1)
	run(FileAction{Action: "copy", IDs: []string{"a.txt", "b.txt"}, Parent: "target"}, 2)
	if _, err = s.FileActions(ctx, a.ID, FileAction{Action: "copy", IDs: []string{"a.txt"}, Parent: "target"}); err == nil {
		t.Fatal("overwrote existing file")
	}
	run(FileAction{Action: "rename", SourceParent: "target", Parent: "target", IDs: []string{"target/a.txt"}, Name: "renamed.txt"}, 1)
	run(FileAction{Action: "move", SourceParent: "target", IDs: []string{"target/renamed.txt"}}, 1)
	run(FileAction{Action: "trash", IDs: []string{"renamed.txt", "b.txt"}}, 2)
	trash, err := os.ReadDir(filepath.Join(root, ".cloud-trash"))
	if err != nil || len(trash) != 2 {
		t.Fatal("trash not recoverable", err)
	}
	data, err := s.List(ctx, a.ID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range data.Items {
		if item.Name == "b.txt" || item.Name == ".cloud-trash" {
			t.Fatal("stale cache or exposed internal trash")
		}
	}
	for _, in := range []FileAction{{Action: "mkdir", Name: "../escape"}, {Action: "move", IDs: []string{"target"}, Parent: "target"}, {Action: "trash", IDs: []string{"missing"}}, {Action: "rename", IDs: []string{"a.txt"}, Name: "target"}} {
		if _, err = s.FileActions(ctx, a.ID, in); err == nil {
			t.Fatalf("accepted unsafe action: %+v", in)
		}
	}
}

func TestQuarkBatchActionsUsePrivateCredentialsAndWaitForCopy(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	a := Account{ID: ID(), Name: "Quark", Provider: "quark", Config: Config{Root: "0", CacheMinutes: 30}}
	if err := s.store.Put(ctx, a, Credentials{Cookie: "test-private-cookie"}); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "test-private-cookie" {
			t.Fatal("missing private credentials")
		}
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/1/clouddrive/file/sort":
			return response(`{"status":200,"data":{"list":[{"fid":"file1","file_name":"one.txt","file_type":1},{"fid":"file2","file_name":"two.txt","file_type":1},{"fid":"dest","file_name":"Target","dir":true}]}}`), nil
		case "/1/clouddrive/file/copy":
			var in struct {
				IDs    []string `json:"filelist"`
				Parent string   `json:"to_pdir_fid"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.IDs) != 2 || in.Parent != "dest" {
				t.Fatal("wrong batch payload")
			}
			return response(`{"status":200,"data":{"task_id":"copy-task"}}`), nil
		case "/1/clouddrive/task":
			return response(`{"status":200,"data":{"status":2}}`), nil
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
			return nil, nil
		}
	})
	// Destination fixture must be empty, while source contains selected entries.
	transport := s.client.Transport
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/file/sort") && r.URL.Query().Get("pdir_fid") == "dest" {
			return response(`{"status":200,"data":{"list":[]}}`), nil
		}
		return transport.RoundTrip(r)
	})
	n, err := s.FileActions(ctx, a.ID, FileAction{Action: "copy", IDs: []string{"file1", "file2"}, Parent: "dest"})
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if len(calls) == 0 || calls[len(calls)-1] != "/1/clouddrive/task" {
		t.Fatal("copy returned before task completion")
	}
	if _, err = s.FileActions(ctx, a.ID, FileAction{Action: "trash", IDs: []string{"outside-root"}}); err == nil {
		t.Fatal("accepted file outside listed source")
	}
}

func TestDownloadHostBoundary(t *testing.T) {
	for _, address := range []string{"http://x.quark.cn/file", "https://127.0.0.1/secret", "https://quark.cn.attacker.test/file", "https://x.quark.cn:8081/file", "https://user@x.quark.cn/file"} {
		if trustedDownloadHost(address) {
			t.Fatal(address)
		}
	}
	if !trustedDownloadHost("https://cdn.quark.cn/file") {
		t.Fatal("rejected quark CDN")
	}
}
