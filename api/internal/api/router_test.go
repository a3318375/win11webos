package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"win11webos/api/internal/storage"
)

func TestHTTPContract(t *testing.T) {
	r := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		method, path string
		status       int
		code         string
	}{{"GET", "/api/health", 200, ""}, {"POST", "/api/v1/accounts", 501, "not_implemented"}, {"GET", "/api/missing", 404, "not_found"}, {"DELETE", "/api/health", 405, "method_not_allowed"}} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status: got %d want %d", w.Code, tc.status)
			}
			var response struct {
				Data  map[string]string `json:"data"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != tc.code {
				t.Fatalf("error code: %q", response.Error.Code)
			}
			if tc.status == 200 && response.Data["status"] != "ok" {
				t.Fatalf("invalid health response: %s", w.Body.String())
			}
		})
	}
}

func TestProviderConfigurationContract(t *testing.T) {
	r := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/providers", nil))
	var response struct {
		Data []storage.Provider `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(response.Data) != 4 {
		t.Fatalf("unexpected providers: %s", w.Body.String())
	}
	for _, p := range response.Data {
		if p.Implemented {
			t.Fatalf("unimplemented provider %s advertised as live", p.ID)
		}
		fields := map[string]storage.Field{}
		for _, f := range p.Fields {
			fields[f.Key] = f
		}
		switch p.ID {
		case "115":
			if !fields["accessToken"].Required || !fields["refreshToken"].Required || fields["downloadMode"].Default != "redirect" {
				t.Fatal("invalid 115 schema")
			}
		case "baidu":
			if fields["accessToken"].Required || !fields["refreshToken"].Required || fields["root"].Default != "/" {
				t.Fatal("invalid Baidu schema")
			}
		case "quark":
			if !fields["cookie"].Required || fields["cookie"].Type != "secret" {
				t.Fatal("invalid Quark schema")
			}
		case "nas":
			if _, ok := fields["downloadMode"]; ok {
				t.Fatal("NAS should not expose download mode")
			}
			if fields["cacheMinutes"].Default != float64(0) {
				t.Fatal("NAS cache should default to zero")
			}
		}
	}
}
