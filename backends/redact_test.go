package backends

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRedactURL_StripsBasicAuth(t *testing.T) {
	in := "https://user:s3cretpass@searx.example.com/search?q=x"
	out := RedactURL(in)
	if strings.Contains(out, "s3cretpass") {
		t.Errorf("password leaked: %q", out)
	}
	if strings.Contains(out, "user:") {
		t.Errorf("userinfo not redacted: %q", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Errorf("expected redaction placeholder, got: %q", out)
	}
	if !strings.Contains(out, "searx.example.com") {
		t.Errorf("host should be preserved: %q", out)
	}
}

func TestRedactURL_NoUserinfoUnchanged(t *testing.T) {
	in := "https://searx.example.com/search?q=x"
	if out := RedactURL(in); out != in {
		t.Errorf("URL without userinfo should be unchanged, got %q", out)
	}
}

func TestRedactSecrets_Bearer(t *testing.T) {
	in := "request failed: Authorization: Bearer tvly-ABCDEF123456 was rejected"
	out := RedactSecrets(in)
	if strings.Contains(out, "tvly-ABCDEF123456") {
		t.Errorf("bearer token leaked: %q", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Errorf("expected redaction, got: %q", out)
	}
}

func TestRedactSecrets_HeaderValues(t *testing.T) {
	cases := []string{
		"x-api-key: sk-live-secret999",
		"X-Subscription-Token: brave-token-xyz",
		"Authorization: Bearer abc.def.ghi",
		"api_key=topsecret",
	}
	secrets := []string{"sk-live-secret999", "brave-token-xyz", "abc.def.ghi", "topsecret"}
	for i, in := range cases {
		out := RedactSecrets(in)
		if strings.Contains(out, secrets[i]) {
			t.Errorf("secret %q leaked in: %q", secrets[i], out)
		}
	}
}

func TestRedactSecrets_EmbeddedURLUserinfo(t *testing.T) {
	in := "dial tcp via https://admin:hunter2@host.local/search failed"
	out := RedactSecrets(in)
	if strings.Contains(out, "hunter2") {
		t.Errorf("embedded url password leaked: %q", out)
	}
}

func TestTruncateBody_CapsLength(t *testing.T) {
	long := strings.Repeat("A", 1000)
	out := TruncateBody(long)
	if len(out) > maxRedactedBodyLen+len("...(truncated)") {
		t.Errorf("body not capped, len=%d", len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation marker, got len=%d", len(out))
	}
}

func TestTruncateBody_RedactsSecrets(t *testing.T) {
	out := TruncateBody("error: Authorization: Bearer leaky-token-123 invalid")
	if strings.Contains(out, "leaky-token-123") {
		t.Errorf("token leaked through TruncateBody: %q", out)
	}
}

// End-to-end: a Tavily 401 with a body echoing the bearer key must not leak the
// key into the returned BackendError message.
func TestRedact_E2E_TavilyAuthErrorNoKeyLeak(t *testing.T) {
	const secretKey = "tvly-SUPERSECRET-DEADBEEF"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo the incoming Authorization header into the body, simulating a
		// chatty upstream error that would leak the key if not redacted.
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid token: " + r.Header.Get("Authorization")))
	}))
	defer srv.Close()

	tv := NewTavilyBackend(secretKey, time.Second, "basic", false, false)
	tv.BaseURL = srv.URL

	_, err := tv.Search(SearchOptions{Query: "x", NumResults: 1})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("API key leaked into error: %q", err.Error())
	}
}

// End-to-end: a SearXNG URL containing basic-auth credentials must not leak the
// password when the request fails (e.g. unreachable host -> network error).
func TestRedact_E2E_SearxngBasicAuthNoLeak(t *testing.T) {
	// Use an unroutable host so the request fails fast; the dial error string
	// could embed the URL with credentials if not redacted.
	s := NewSearxngBackend("http://user:topsecretpw@127.0.0.1:1/", "", "", "GET", 500*time.Millisecond, false, false)
	_, err := s.Search(SearchOptions{Query: "x"})
	if err == nil {
		t.Skip("expected the request to fail; environment returned success")
	}
	if strings.Contains(err.Error(), "topsecretpw") {
		t.Fatalf("basic-auth password leaked into error: %q", err.Error())
	}
}
