package mount

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"testing"
)

func TestQuarkQRCodeMatchesOfficialLoginParameters(t *testing.T) {
	u, err := url.Parse(quarkQRURL("test-token-+/="))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for key, want := range map[string]string{"token": "test-token-+/=", "client_id": "532", "ssb": "weblogin", "uc_param_str": "", "uc_biz_str": "S:custom|OPT:SAREA@0|OPT:IMMERSIVE@1|OPT:BACK_BTN_STYLE@0"} {
		got, present := q[key]
		if !present || len(got) != 1 || got[0] != want {
			t.Fatalf("QR parameter %s missing or incorrect", key)
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(quarkRequestID()) {
		t.Fatal("request_id is not UUID v4")
	}
}

func TestQuarkExpiredStateAndMessagePersist(t *testing.T) {
	s, _, _ := setup(t)
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") != quarkDesktopUA {
			t.Error("inconsistent browser identity")
		}
		if r.URL.Query().Get("token") == "" {
			return response(`{"status":2000000,"data":{"members":{"token":"test-token"}}}`), nil
		}
		return response(`{"status":50004002}`), nil
	})
	session, err := s.StartAuth(context.Background(), "quark")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		state, err := s.PollAuth(context.Background(), session.ID)
		if err != nil || state.Status != "expired" || state.Message == "" {
			t.Fatal("expired state/message not retained", err)
		}
	}
}

// Explicit opt-in: only obtains an anonymous QR session, never scans or logs in.
func TestQuarkLiveAnonymousSession(t *testing.T) {
	if os.Getenv("CLOUD_LIVE_QUARK") != "1" {
		t.Skip("set CLOUD_LIVE_QUARK=1 for anonymous upstream smoke test")
	}
	s, _, _ := setup(t)
	session, err := s.StartAuth(context.Background(), "quark")
	if err != nil {
		t.Fatal(err)
	}
	defer s.CancelAuth(session.ID)
	state, err := s.PollAuth(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "pending" {
		t.Fatalf("new anonymous session unexpectedly %s", state.Status)
	}
	t.Log("live upstream accepted new QR session and reports waiting for scan; no login attempted")
}
