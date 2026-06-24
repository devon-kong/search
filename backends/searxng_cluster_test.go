package backends

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMultiSearxngBackend_IsAvailable(t *testing.T) {
	b := NewMultiSearxngBackend(
		[]string{"", "not-a-url"},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyOrdered,
	)

	if b.IsAvailable() {
		t.Fatal("expected backend to be unavailable")
	}
}

func TestMultiSearxngBackend_SearchOrderedFallsBack(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer failing.Close()

	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := SearxngResponse{
			Results: []searxngResult{{
				Title: "fallback result",
				URL:   "https://example.com/fallback",
			}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer working.Close()

	b := NewMultiSearxngBackend(
		[]string{failing.URL, working.URL},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyOrdered,
	)

	results, err := b.Search(SearchOptions{Query: "test"})
	if err != nil {
		t.Fatalf("expected successful fallback, got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "fallback result" {
		t.Fatalf("unexpected result title: %q", results[0].Title)
	}
}

func TestMultiSearxngBackend_SearchRawOrderedFallsBack(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer failing.Close()

	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"title":"raw fallback","url":"https://example.com/raw"}],"number_of_results":1}`))
	}))
	defer working.Close()

	b := NewMultiSearxngBackend(
		[]string{failing.URL, working.URL},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyOrdered,
	)

	raw, err := b.SearchRaw(SearchOptions{Query: "test"})
	if err != nil {
		t.Fatalf("expected successful raw fallback, got error: %v", err)
	}
	if len(raw.Results) != 1 || raw.Results[0].Title != "raw fallback" {
		t.Fatalf("unexpected raw fallback results: %#v", raw.Results)
	}
	if raw.Diagnostics.NumberOfResults != 1 {
		t.Fatalf("number_of_results = %d, want 1", raw.Diagnostics.NumberOfResults)
	}
}

func TestMultiSearxngBackend_SearchParallelFastest(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		resp := SearxngResponse{Results: []searxngResult{{Title: "slow", URL: "https://example.com/slow"}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer slow.Close()

	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := SearxngResponse{Results: []searxngResult{{Title: "fast", URL: "https://example.com/fast"}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer fast.Close()

	b := NewMultiSearxngBackend(
		[]string{slow.URL, fast.URL},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyParallelFastest,
	)

	results, err := b.Search(SearchOptions{Query: "test"})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "fast" {
		t.Fatalf("expected fastest result, got %q", results[0].Title)
	}
}

func TestMultiSearxngBackend_SearchRawParallelFastest(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte(`{"results":[{"title":"slow","url":"https://example.com/slow"}]}`))
	}))
	defer slow.Close()

	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"title":"fast raw","url":"https://example.com/fast"}]}`))
	}))
	defer fast.Close()

	b := NewMultiSearxngBackend(
		[]string{slow.URL, fast.URL},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyParallelFastest,
	)

	raw, err := b.SearchRaw(SearchOptions{Query: "test"})
	if err != nil {
		t.Fatalf("expected raw success, got error: %v", err)
	}
	if len(raw.Results) != 1 || raw.Results[0].Title != "fast raw" {
		t.Fatalf("expected fastest raw result, got %#v", raw.Results)
	}
}

// --- MultiSearxngBackend.FetchConfig, all via httptest ---------------------

// configBodyWithEngine returns a minimal valid /config body whose single engine
// has the given name, so tests can prove WHICH instance answered.
func configBodyWithEngine(name string) string {
	return `{"engines":[{"name":"` + name + `","shortcut":"x","categories":["general"],"enabled":true,"timeout":3.0,"paging":true,"safesearch":false,"time_range_support":false,"language_support":true}]}`
}

func TestMultiSearxngBackend_FetchConfigOrderedFallsBack(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer failing.Close()

	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(configBodyWithEngine("from-second")))
	}))
	defer working.Close()

	b := NewMultiSearxngBackend(
		[]string{failing.URL, working.URL},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyOrdered,
	)

	resp, err := b.FetchConfig()
	if err != nil {
		t.Fatalf("expected successful ordered fallback, got error: %v", err)
	}
	if len(resp.Engines) != 1 || resp.Engines[0].Name != "from-second" {
		t.Fatalf("expected engine from the second instance, got %#v", resp.Engines)
	}
}

func TestMultiSearxngBackend_FetchConfigOrderedAllFail(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer failing.Close()

	failing2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer failing2.Close()

	b := NewMultiSearxngBackend(
		[]string{failing.URL, failing2.URL},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyOrdered,
	)

	if _, err := b.FetchConfig(); err == nil {
		t.Fatal("expected error when all instances fail")
	}
}

func TestMultiSearxngBackend_FetchConfigParallelFastest(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte(configBodyWithEngine("slow-engine")))
	}))
	defer slow.Close()

	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(configBodyWithEngine("fast-engine")))
	}))
	defer fast.Close()

	b := NewMultiSearxngBackend(
		[]string{slow.URL, fast.URL},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyParallelFastest,
	)

	resp, err := b.FetchConfig()
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(resp.Engines) != 1 || resp.Engines[0].Name != "fast-engine" {
		t.Fatalf("expected engine from the fast instance, got %#v", resp.Engines)
	}
}

// When the fast instance fails immediately, the slower-but-successful instance
// must still produce an overall success.
func TestMultiSearxngBackend_FetchConfigParallelFastestFastFailsSlowSucceeds(t *testing.T) {
	fastFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer fastFail.Close()

	slowOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte(configBodyWithEngine("slow-but-ok")))
	}))
	defer slowOK.Close()

	b := NewMultiSearxngBackend(
		[]string{fastFail.URL, slowOK.URL},
		"", "", "GET", 2*time.Second, false, false,
		SearxngStrategyParallelFastest,
	)

	resp, err := b.FetchConfig()
	if err != nil {
		t.Fatalf("expected overall success from the slow instance, got error: %v", err)
	}
	if len(resp.Engines) != 1 || resp.Engines[0].Name != "slow-but-ok" {
		t.Fatalf("expected engine from the slow successful instance, got %#v", resp.Engines)
	}
}

func TestDeduplicateSearxngURLs(t *testing.T) {
	urls := []string{"", "https://a.example", "https://a.example", "https://b.example"}
	got := DeduplicateSearxngURLs(urls)
	if len(got) != 2 {
		t.Fatalf("expected 2 urls, got %d", len(got))
	}
	if got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("unexpected deduped urls: %#v", got)
	}
}
