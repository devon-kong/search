package backends

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearxngBackend_Name(t *testing.T) {
	b := NewSearxngBackend("http://localhost", "", "", "GET", 10*time.Second, false, false)
	if b.Name() != "searxng" {
		t.Errorf("expected 'searxng', got %q", b.Name())
	}
}

func TestSearxngBackend_IsAvailable(t *testing.T) {
	tests := []struct {
		baseURL string
		want    bool
	}{
		{"", false},
		{"not-a-url", false},
		{"http://localhost:8888", true},
		{"https://searx.example.com", true},
	}
	for _, tt := range tests {
		b := NewSearxngBackend(tt.baseURL, "", "", "GET", 10*time.Second, false, false)
		if got := b.IsAvailable(); got != tt.want {
			t.Errorf("IsAvailable(%q) = %v, want %v", tt.baseURL, got, tt.want)
		}
	}
}

func TestSearxngBackend_Search_Unavailable(t *testing.T) {
	b := NewSearxngBackend("", "", "", "GET", 10*time.Second, false, false)
	_, err := b.Search(SearchOptions{Query: "test"})
	if err == nil {
		t.Fatal("expected error for unavailable backend")
	}
}

func TestSearxngBackend_Search_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Query().Get("q") != "golang" {
			t.Errorf("expected query 'golang', got %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("expected format 'json', got %q", r.URL.Query().Get("format"))
		}

		resp := SearxngResponse{
			Results: []searxngResult{
				{
					Title:   "Go Dev",
					URL:     "https://go.dev",
					Content: "Official Go site",
					Engines: []string{"google", "duckduckgo"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// The server URL includes no /search path, so we remove the trailing slash
	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	results, err := b.Search(SearchOptions{Query: "golang"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Go Dev" {
		t.Errorf("expected 'Go Dev', got %q", results[0].Title)
	}
}

func TestSearxngBackend_SearchRaw_ReturnsRawAndDiagnostics(t *testing.T) {
	rawBody := `{
		"results": [
			{
				"title": "Go Dev",
				"url": "https://go.dev",
				"content": "Official Go site",
				"engine": "google",
				"engines": ["google"]
			}
		],
		"answers": [{"answer": "42"}],
		"suggestions": ["golang"],
		"infoboxes": [{"id": "go"}],
		"unresponsive_engines": [["bing", "timeout"]],
		"number_of_results": 123
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rawBody))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	raw, err := b.SearchRaw(SearchOptions{Query: "golang"})
	if err != nil {
		t.Fatalf("SearchRaw failed: %v", err)
	}
	if string(raw.Raw) != rawBody {
		t.Fatalf("raw body mismatch:\ngot  %s\nwant %s", raw.Raw, rawBody)
	}
	if len(raw.Results) != 1 || raw.Results[0].Title != "Go Dev" {
		t.Fatalf("unexpected parsed results: %#v", raw.Results)
	}
	if raw.Diagnostics.NumberOfResults != 123 {
		t.Fatalf("number_of_results = %d, want 123", raw.Diagnostics.NumberOfResults)
	}
	if string(raw.Diagnostics.Answers) != `[{"answer": "42"}]` {
		t.Fatalf("answers raw mismatch: %s", raw.Diagnostics.Answers)
	}
	if string(raw.Diagnostics.UnresponsiveEngines) != `[["bing", "timeout"]]` {
		t.Fatalf("unresponsive raw mismatch: %s", raw.Diagnostics.UnresponsiveEngines)
	}
}

func TestSearxngBackend_SearchRaw_MissingDiagnosticsDefaultsToArrays(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	raw, err := b.SearchRaw(SearchOptions{Query: "empty"})
	if err != nil {
		t.Fatalf("SearchRaw failed: %v", err)
	}
	if len(raw.Results) != 0 {
		t.Fatalf("expected empty results, got %#v", raw.Results)
	}
	if string(raw.Diagnostics.Answers) != `[]` ||
		string(raw.Diagnostics.Suggestions) != `[]` ||
		string(raw.Diagnostics.Infoboxes) != `[]` ||
		string(raw.Diagnostics.UnresponsiveEngines) != `[]` {
		t.Fatalf("missing diagnostics should default to arrays, got %#v", raw.Diagnostics)
	}
}

func TestSearxngBackend_Search_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("expected form content-type, got %q", r.Header.Get("Content-Type"))
		}

		r.ParseForm()
		if r.FormValue("q") != "test" {
			t.Errorf("expected query 'test', got %q", r.FormValue("q"))
		}

		resp := SearxngResponse{
			Results: []searxngResult{
				{Title: "POST Result", URL: "https://post.com"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "POST", 10*time.Second, false, false)
	results, err := b.Search(SearchOptions{Query: "test"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 || results[0].Title != "POST Result" {
		t.Errorf("unexpected results: %v", results)
	}
}

func TestSearxngBackend_Search_WithBasicAuth(t *testing.T) {
	var capturedUser, capturedPass string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser, capturedPass, _ = r.BasicAuth()

		resp := SearxngResponse{Results: []searxngResult{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "user", "pass", "GET", 10*time.Second, false, false)
	b.Search(SearchOptions{Query: "test"})

	if capturedUser != "user" || capturedPass != "pass" {
		t.Errorf("expected user/pass, got %q/%q", capturedUser, capturedPass)
	}
}

func TestSearxngBackend_Search_WithSiteFilter(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("q")
		resp := SearxngResponse{Results: []searxngResult{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	b.Search(SearchOptions{Query: "test", Site: "example.com"})

	if capturedQuery != "site:example.com test" {
		t.Errorf("expected 'site:example.com test', got %q", capturedQuery)
	}
}

func TestSearxngBackend_Search_WithCategories(t *testing.T) {
	var capturedCategories string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCategories = r.URL.Query().Get("categories")
		resp := SearxngResponse{Results: []searxngResult{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	b.Search(SearchOptions{Query: "test", Categories: []string{"news", "social-media"}})

	if capturedCategories != "news,social media" {
		t.Errorf("expected 'news,social media', got %q", capturedCategories)
	}
}

func TestSearxngBackend_Search_WithTimeRange(t *testing.T) {
	var capturedTimeRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTimeRange = r.URL.Query().Get("time_range")
		resp := SearxngResponse{Results: []searxngResult{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	b.Search(SearchOptions{Query: "test", TimeRange: "week"})

	if capturedTimeRange != "week" {
		t.Errorf("expected 'week', got %q", capturedTimeRange)
	}
}

func TestSearxngBackend_Search_WithSearxngOptions(t *testing.T) {
	var captured map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = map[string]string{
			"engines":     r.URL.Query().Get("engines"),
			"language":    r.URL.Query().Get("language"),
			"safesearch":  r.URL.Query().Get("safesearch"),
			"time_range":  r.URL.Query().Get("time_range"),
			"pageno":      r.URL.Query().Get("pageno"),
			"num":         r.URL.Query().Get("num"),
			"categories":  r.URL.Query().Get("categories"),
			"format":      r.URL.Query().Get("format"),
			"user-agent":  r.Header.Get("User-Agent"),
			"accept":      r.Header.Get("Accept"),
			"http_method": r.Method,
		}
		resp := SearxngResponse{Results: []searxngResult{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	_, err := b.SearchRaw(SearchOptions{
		Query:      "test",
		Categories: []string{"news"},
		Engines:    []string{"google", "google news"},
		Language:   "en-US",
		SafeSearch: "moderate",
		TimeRange:  "month",
		PageNo:     2,
		NumResults: 7,
	})
	if err != nil {
		t.Fatalf("SearchRaw failed: %v", err)
	}

	want := map[string]string{
		"engines":     "google,google news",
		"language":    "en-US",
		"safesearch":  "1",
		"time_range":  "month",
		"pageno":      "2",
		"num":         "7",
		"categories":  "news",
		"format":      "json",
		"user-agent":  "sx/2.0",
		"accept":      "application/json",
		"http_method": "GET",
	}
	for key, wantValue := range want {
		if captured[key] != wantValue {
			t.Fatalf("%s = %q, want %q (all captured: %#v)", key, captured[key], wantValue, captured)
		}
	}
}

func TestSearxngBackend_Search_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	_, err := b.Search(SearchOptions{Query: "test"})
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestSearxngBackend_Search_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	_, err := b.Search(SearchOptions{Query: "test"})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSearxngBackend_Search_UserAgent(t *testing.T) {
	var capturedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		resp := SearxngResponse{Results: []searxngResult{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// With user agent
	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	b.Search(SearchOptions{Query: "test"})
	if capturedUA != "sx/2.0" {
		t.Errorf("expected 'sx/2.0', got %q", capturedUA)
	}

	// Without user agent
	b = NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, true)
	b.Search(SearchOptions{Query: "test"})
	if capturedUA == "sx/2.0" {
		t.Error("expected no user agent when NoUserAgent=true")
	}
}

// --- FetchConfig (single instance), all via httptest -----------------------

// validConfigBody is a small valid /config response with two engines used by the
// FetchConfig tests. The first engine exercises all 9 fields with non-zero values.
const validConfigBody = `{
	"engines": [
		{
			"name": "google",
			"shortcut": "go",
			"categories": ["general", "web"],
			"enabled": true,
			"timeout": 3.0,
			"paging": true,
			"safesearch": true,
			"time_range_support": true,
			"language_support": true
		},
		{
			"name": "bing",
			"shortcut": "bi",
			"categories": ["general"],
			"enabled": false,
			"timeout": 20.0,
			"paging": false,
			"safesearch": false,
			"time_range_support": false,
			"language_support": false
		}
	]
}`

func TestSearxngBackend_FetchConfig_GETOnlyNoSearchParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/config" {
			t.Errorf("expected path /config, got %q", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("expected no query params, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validConfigBody))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	resp, err := b.FetchConfig()
	if err != nil {
		t.Fatalf("FetchConfig failed: %v", err)
	}
	if len(resp.Engines) != 2 {
		t.Fatalf("expected 2 engines, got %d", len(resp.Engines))
	}
}

// FetchConfig must always use GET even when the backend is configured for POST.
func TestSearxngBackend_FetchConfig_AlwaysGETEvenWhenPOSTConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET for /config even with POST backend, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validConfigBody))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "POST", 10*time.Second, false, false)
	if _, err := b.FetchConfig(); err != nil {
		t.Fatalf("FetchConfig failed: %v", err)
	}
}

func TestSearxngBackend_FetchConfig_FieldParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validConfigBody))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	resp, err := b.FetchConfig()
	if err != nil {
		t.Fatalf("FetchConfig failed: %v", err)
	}
	if len(resp.Engines) < 1 {
		t.Fatalf("expected at least 1 engine")
	}
	e := resp.Engines[0]
	if e.Name != "google" {
		t.Errorf("name = %q, want google", e.Name)
	}
	if e.Shortcut != "go" {
		t.Errorf("shortcut = %q, want go", e.Shortcut)
	}
	if len(e.Categories) != 2 || e.Categories[0] != "general" || e.Categories[1] != "web" {
		t.Errorf("categories = %#v, want [general web]", e.Categories)
	}
	if !e.Enabled {
		t.Errorf("enabled = %v, want true", e.Enabled)
	}
	if e.Timeout != 3.0 {
		t.Errorf("timeout = %v, want 3.0", e.Timeout)
	}
	if !e.Paging {
		t.Errorf("paging = %v, want true", e.Paging)
	}
	if !e.SafeSearch {
		t.Errorf("safesearch = %v, want true", e.SafeSearch)
	}
	if !e.TimeRangeSupport {
		t.Errorf("time_range_support = %v, want true", e.TimeRangeSupport)
	}
	if !e.LanguageSupport {
		t.Errorf("language_support = %v, want true", e.LanguageSupport)
	}
}

func TestSearxngBackend_FetchConfig_BasicAuth(t *testing.T) {
	var capturedUser, capturedPass string
	var hadAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser, capturedPass, hadAuth = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validConfigBody))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "user", "pass", "GET", 10*time.Second, false, false)
	if _, err := b.FetchConfig(); err != nil {
		t.Fatalf("FetchConfig failed: %v", err)
	}
	if !hadAuth {
		t.Fatal("expected basic auth to be set")
	}
	if capturedUser != "user" || capturedPass != "pass" {
		t.Errorf("expected user/pass, got %q/%q", capturedUser, capturedPass)
	}
}

func TestSearxngBackend_FetchConfig_UserAgent(t *testing.T) {
	var capturedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validConfigBody))
	}))
	defer server.Close()

	// Default sends sx/2.0.
	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	if _, err := b.FetchConfig(); err != nil {
		t.Fatalf("FetchConfig failed: %v", err)
	}
	if capturedUA != "sx/2.0" {
		t.Errorf("expected 'sx/2.0', got %q", capturedUA)
	}

	// With NoUserAgent=true the UA is NOT sx/2.0.
	b = NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, true)
	if _, err := b.FetchConfig(); err != nil {
		t.Fatalf("FetchConfig failed: %v", err)
	}
	if capturedUA == "sx/2.0" {
		t.Errorf("expected no sx/2.0 user agent when NoUserAgent=true, got %q", capturedUA)
	}
}

func TestSearxngBackend_FetchConfig_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	if _, err := b.FetchConfig(); err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestSearxngBackend_FetchConfig_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	b := NewSearxngBackend(server.URL, "", "", "GET", 10*time.Second, false, false)
	if _, err := b.FetchConfig(); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSearxngBackend_FetchConfig_UnconfiguredURL(t *testing.T) {
	b := NewSearxngBackend("", "", "", "GET", 10*time.Second, false, false)
	_, err := b.FetchConfig()
	if err == nil {
		t.Fatal("expected error for unconfigured URL")
	}
	var be *BackendError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BackendError, got %T: %v", err, err)
	}
	if be.Code != ErrCodeUnavailable {
		t.Errorf("Code = %d, want ErrCodeUnavailable (%d)", be.Code, ErrCodeUnavailable)
	}
}

func TestNormalizeCategory(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"social-media", "social media"},
		{"social+media", "social media"},
		{"social_media", "social media"},
		{"socialmedia", "social media"},
		{"news", "news"},
		{"general", "general"},
	}
	for _, tt := range tests {
		if got := normalizeCategory(tt.input); got != tt.want {
			t.Errorf("normalizeCategory(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
