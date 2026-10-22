package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"win11webos/api/internal/mount"
)

func TestMountHTTPWorkflow(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "files")
	os.Mkdir(root, 0700)
	os.WriteFile(filepath.Join(root, "hello.txt"), []byte("abcdef"), 0600)
	store, err := mount.OpenStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := mount.New(store, []string{root}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	payload, _ := json.Marshal(mount.Input{Name: "NAS", Provider: "nas", Config: mount.Config{Root: root, CacheMinutes: 30}})
	request := httptest.NewRequest("POST", "/api/v1/accounts", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created struct {
		Data mount.Account `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/accounts/"+created.Data.ID+"/files", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "hello.txt") {
		t.Fatal(w.Code, w.Body.String())
	}
	request = httptest.NewRequest("GET", "/api/v1/accounts/"+created.Data.ID+"/content?id=hello.txt", nil)
	request.Header.Set("Range", "bytes=1-3")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != 206 || w.Body.String() != "bcd" {
		t.Fatal("Range broken", w.Code, w.Body.String())
	}
	request = httptest.NewRequest("POST", "/api/v1/accounts", bytes.NewReader(payload))
	request.Header.Set("Origin", "https://untrusted.test")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("cross origin write accepted")
	}
}
