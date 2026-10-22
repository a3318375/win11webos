package mount

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func setup(t *testing.T) (*Service, string, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "files")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	store, err := OpenStore(data)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store, []string{root}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, root, data
}
func TestLocalMountCacheRefreshAndConfinement(t *testing.T) {
	s, root, _ := setup(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0600)
	os.Mkdir(filepath.Join(root, "子目录"), 0700)
	os.WriteFile(filepath.Join(root, "子目录", "child.txt"), []byte("child"), 0600)
	a, err := s.Create(ctx, Input{Name: "我的 NAS", Provider: "nas", Config: Config{Root: root, CacheMinutes: 30}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.List(ctx, a.ID, "", false)
	if err != nil || first.CacheHit || len(first.Items) != 2 {
		t.Fatalf("first: %+v %v", first, err)
	}
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0600)
	cached, err := s.List(ctx, a.ID, "", false)
	if err != nil || !cached.CacheHit || len(cached.Items) != 2 {
		t.Fatal("cache not honored", err)
	}
	fresh, err := s.List(ctx, a.ID, "", true)
	if err != nil || fresh.CacheHit || len(fresh.Items) != 3 {
		t.Fatal("refresh failed", err)
	}
	sub, err := s.List(ctx, a.ID, "子目录", false)
	if err != nil || len(sub.Items) != 1 || sub.Items[0].Name != "child.txt" {
		t.Fatal("subdirectory failed", err)
	}
	if _, err = s.List(ctx, a.ID, "../", false); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err = s.List(ctx, a.ID, filepath.Dir(root), false); err == nil {
		t.Fatal("absolute path accepted")
	}
	if _, err = s.Create(ctx, Input{Name: "outside", Provider: "nas", Config: Config{Root: filepath.Dir(root)}}); err == nil {
		t.Fatal("root allowlist bypass")
	}
	file, err := s.OpenContent(ctx, a.ID, "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(file)
	file.Close()
	if string(body) != "hello" {
		t.Fatal("wrong content")
	}
	if _, err = s.OpenContent(ctx, a.ID, "../data/cloud.db"); err == nil {
		t.Fatal("content traversal accepted")
	}
	if err = s.Remove(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "hello.txt")); err != nil {
		t.Fatal("disconnect removed source")
	}
}
func TestZeroCacheAndEmptyDirectory(t *testing.T) {
	s, root, _ := setup(t)
	ctx := context.Background()
	a, err := s.Create(ctx, Input{Name: "uncached", Provider: "nas", Config: Config{Root: root}})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s.List(ctx, a.ID, "", false)
	if first.Items == nil {
		t.Fatal("empty arrays must serialize as []")
	}
	os.WriteFile(filepath.Join(root, "new"), []byte{}, 0600)
	next, _ := s.List(ctx, a.ID, "", false)
	if next.CacheHit || len(next.Items) != 1 {
		t.Fatal("zero TTL cached results")
	}
}
func TestEncryptedPersistenceAndNoCredentialDTO(t *testing.T) {
	s, _, data := setup(t)
	ctx := context.Background()
	creds := Credentials{Access: "secret-access-marker", Refresh: "secret-refresh-marker", Cookie: "secret-cookie-marker"}
	a := Account{ID: ID(), Name: "persistent", Provider: "115", CreatedAt: time.Now(), Config: Config{Root: "0"}}
	if err := s.store.Put(ctx, a, creds); err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err := s.store.db.QueryRow(`SELECT secret FROM accounts WHERE id=?`, a.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("secret-")) {
		t.Fatal("credentials stored plaintext")
	}
	accounts, _ := s.Accounts(ctx)
	body, _ := json.Marshal(accounts)
	if bytes.Contains(body, []byte("secret-")) {
		t.Fatal("credentials leaked in response")
	}
	other, err := OpenStore(data)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	_, restored, err := other.Get(ctx, a.ID)
	if err != nil || restored != creds {
		t.Fatal("credentials not restored", err)
	}
	a.ID = ID()
	a.Name = "PERSISTENT"
	if err = other.Put(ctx, a, creds); err == nil {
		t.Fatal("case insensitive duplicate allowed")
	}
}
func TestRemoteCoalescingAndNavigationBoundary(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	var calls atomic.Int32
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		time.Sleep(30 * time.Millisecond)
		return response(`{"state":true,"count":1,"data":[{"fid":"folder-1","fn":"目录","fc":"0","fs":"0","upt":"1700000000"}]}`), nil
	})
	a := Account{ID: ID(), Name: "115", Provider: "115", Config: Config{Root: "0", CacheMinutes: 30}, CreatedAt: time.Now()}
	if err := s.store.Put(ctx, a, Credentials{Access: "token"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.List(ctx, a.ID, "", false)
			if err != nil || len(result.Items) != 1 {
				t.Error("list failed", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("upstream calls: %d", calls.Load())
	}
	if _, err := s.List(ctx, a.ID, "arbitrary-folder", false); err == nil {
		t.Fatal("unvisited directory accepted")
	}
	if _, err := s.List(ctx, a.ID, "folder-1", false); err != nil {
		t.Fatal(err)
	}
}
func TestRefreshCredentialsRetainedBetweenTestAndSave(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "refreshToken") {
			return response(`{"state":true,"data":{"access_token":"new-access","refresh_token":"new-refresh"}}`), nil
		}
		if r.Header.Get("Authorization") != "Bearer new-access" {
			t.Error("wrong bearer token")
		}
		return response(`{"state":true,"count":0,"data":[]}`), nil
	})
	input := Input{Name: "115", Provider: "115", Config: Config{Root: "0"}, Credentials: Credentials{Refresh: "old-refresh"}}
	normalized, _, err := s.Test(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	session := s.RetainTest(input.Provider, normalized.Credentials, "")
	input.Credentials = Credentials{}
	input.AuthSession = session.ID
	a, err := s.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	_, saved, err := s.store.Get(ctx, a.ID)
	if err != nil || saved.Refresh != "new-refresh" {
		t.Fatal("rotated credentials lost", err)
	}
	if _, err = s.sessionCredentials(session.ID, "115"); err == nil {
		t.Fatal("completed session not consumed")
	}
}
func TestAuthBrokerKeepsCredentialsServerSide(t *testing.T) {
	s, _, _ := setup(t)
	s.broker = "https://auth.test"
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			return response(`{"success":true,"data":{"session_id":"upstream","oauth_url":"https://provider.test/authorize"}}`), nil
		case strings.Contains(r.URL.Path, "/status/"):
			return response(`{"success":true,"data":{"status":"success","token_data":{"access_token":"private-token","refresh_token":"private-refresh"}}}`), nil
		default:
			return response(`{"success":true}`), nil
		}
	})
	session, err := s.StartAuth(context.Background(), "115")
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.PollAuth(context.Background(), session.ID)
	if err != nil || status.Status != "success" {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(status)
	if bytes.Contains(encoded, []byte("private-")) {
		t.Fatal("long term credentials leaked")
	}
	if _, err = s.sessionCredentials(session.ID, "baidu"); err == nil {
		t.Fatal("cross-provider session accepted")
	}
	s.CancelAuth(session.ID)
	if _, err = s.PollAuth(context.Background(), session.ID); err == nil {
		t.Fatal("cancelled session exists")
	}
}

func TestQuarkQRAndExpiredSessions(t *testing.T) {
	s, _, _ := setup(t)
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.Path, "getTokenForQrcodeLogin"):
			return response(`{"status":2000000,"data":{"members":{"token":"qr-session-token"}}}`), nil
		case strings.Contains(r.URL.Path, "getServiceTicketByQrcodeToken"):
			return response(`{"status":2000000,"data":{"members":{"service_ticket":"ticket"}}}`), nil
		case r.URL.Path == "/account/info":
			res := response(`{"status":2000000}`)
			res.Header.Add("Set-Cookie", "__puus=private-cookie; Domain=.quark.cn; Path=/; Secure")
			return res, nil
		case strings.Contains(r.URL.Path, "file/sort"):
			return response(`{"status":200,"data":{"list":[]}}`), nil
		default:
			return response(`{}`), nil
		}
	})
	session, err := s.StartAuth(context.Background(), "quark")
	if err != nil || !strings.HasPrefix(session.QR, "data:image/png;base64,") {
		t.Fatal("QR failed", err)
	}
	status, err := s.PollAuth(context.Background(), session.ID)
	if err != nil || status.Status != "success" {
		t.Fatal("poll failed", err)
	}
	creds, err := s.sessionCredentials(session.ID, "quark")
	if err != nil || !strings.Contains(creds.Cookie, "__puus=private-cookie") {
		t.Fatal("cookie not retained", err)
	}
	encoded, _ := json.Marshal(status)
	if bytes.Contains(encoded, []byte("private-cookie")) {
		t.Fatal("cookie exposed")
	}
	s.authMu.Lock()
	s.sessions[session.ID].Expires = time.Now().Add(-time.Minute)
	s.authMu.Unlock()
	expired, err := s.PollAuth(context.Background(), session.ID)
	if err != nil || expired.Status != "expired" {
		t.Fatal("expiry failed", err)
	}
}

func TestRefreshPersistsEvenWhenDirectoryFails(t *testing.T) {
	s, _, _ := setup(t)
	a := Account{ID: ID(), Name: "refresh", Provider: "115", Config: Config{Root: "0"}, CreatedAt: time.Now()}
	s.store.Put(context.Background(), a, Credentials{Access: "expired", Refresh: "old"})
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "refreshToken") {
			return response(`{"state":1,"data":{"access_token":"updated","refresh_token":"rotated"}}`), nil
		}
		if r.Header.Get("Authorization") == "Bearer expired" {
			res := response(`{}`)
			res.StatusCode = 401
			return res, nil
		}
		return response(`{"state":false,"code":20013}`), nil
	})
	if _, err := s.List(context.Background(), a.ID, "", false); err == nil {
		t.Fatal("permission failure not propagated")
	}
	_, c, err := s.store.Get(context.Background(), a.ID)
	if err != nil || c.Refresh != "rotated" {
		t.Fatal("new refresh token lost", err)
	}
}

func TestRemoteBaiduErrorsCannotCreateFalseMount(t *testing.T) {
	s, _, _ := setup(t)
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(`{"error_code":110,"error_msg":"invalid token"}`), nil
	})
	_, err := s.Create(context.Background(), Input{Name: "bad", Provider: "baidu", Config: Config{Root: "/"}, Credentials: Credentials{Access: "invalid"}})
	if err == nil {
		t.Fatal("invalid token accepted")
	}
	accounts, _ := s.Accounts(context.Background())
	if len(accounts) != 0 {
		t.Fatal("failed mount saved")
	}
}

func TestQuarkDirectoryPagination(t *testing.T) {
	s, _, _ := setup(t)
	var calls int
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "test-cookie" {
			t.Error("cookie missing")
		}
		count := 100
		if r.URL.Query().Get("_page") == "2" {
			count = 1
		}
		rows := []map[string]any{}
		for i := 0; i < count; i++ {
			rows = append(rows, map[string]any{"fid": ID(), "file_name": "file.txt", "file_type": 1, "size": 10, "updated_at": 1700000000000})
		}
		body, _ := json.Marshal(map[string]any{"status": 200, "data": map[string]any{"list": rows}, "metadata": map[string]any{"_total": 101}})
		return response(string(body)), nil
	})
	items, _, err := s.remoteList(context.Background(), Account{Provider: "quark", Config: Config{Root: "0"}}, Credentials{Cookie: "test-cookie"})
	if err != nil || len(items) != 101 || calls != 2 {
		t.Fatal("pagination failed", len(items), calls, err)
	}
}
