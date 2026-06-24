package backends

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- CostTier values per backend (incl. exa dynamic by mode) ----------------

func TestCostTier_Searxng(t *testing.T) {
	s := NewSearxngBackend("https://searx.example.com", "", "", "GET", time.Second, false, false)
	if got := s.CostTier(); got != CostTierSelfHosted {
		t.Errorf("searxng CostTier = %q, want %q", got, CostTierSelfHosted)
	}
	m := NewMultiSearxngBackend([]string{"https://searx.example.com"}, "", "", "GET", time.Second, false, false, SearxngStrategyOrdered)
	if got := m.CostTier(); got != CostTierSelfHosted {
		t.Errorf("multi searxng CostTier = %q, want %q", got, CostTierSelfHosted)
	}
}

func TestCostTier_Brave(t *testing.T) {
	b := NewBraveBackend("key", time.Second)
	if got := b.CostTier(); got != CostTierFreeExternal {
		t.Errorf("brave CostTier = %q, want %q", got, CostTierFreeExternal)
	}
}

func TestCostTier_Jina(t *testing.T) {
	j := NewJinaBackend("", time.Second, true, "")
	if got := j.CostTier(); got != CostTierFreeExternal {
		t.Errorf("jina CostTier = %q, want %q", got, CostTierFreeExternal)
	}
}

func TestCostTier_Tavily(t *testing.T) {
	tv := NewTavilyBackend("key", time.Second, "basic", false, false)
	if got := tv.CostTier(); got != CostTierPaid {
		t.Errorf("tavily CostTier = %q, want %q", got, CostTierPaid)
	}
}

func TestCostTier_Exa_DynamicByMode(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{ExaModeAPI, CostTierPaid},
		{ExaModeMCP, CostTierFreeExternal},
		{ExaModeAuto, CostTierPaid}, // conservative: auto tries paid API first
		{"", CostTierPaid},          // empty normalizes to auto
	}
	for _, c := range cases {
		e := NewExaBackend(c.mode, "key", time.Second, "https://mcp.example.com", "exa-web-search", 10)
		if got := e.CostTier(); got != c.want {
			t.Errorf("exa(mode=%q) CostTier = %q, want %q", c.mode, got, c.want)
		}
	}
}

// --- paid fallback opt-in gate (the security-critical behavior) -------------

// countingBackend records how many times Search was called, so we can assert a
// paid backend is never touched when the gate is closed.
type countingBackend struct {
	name     string
	tier     string
	calls    int
	results  []SearchResult
	failWith error
}

func (c *countingBackend) Name() string      { return c.name }
func (c *countingBackend) IsAvailable() bool { return true }
func (c *countingBackend) CostTier() string  { return c.tier }
func (c *countingBackend) Search(opts SearchOptions) ([]SearchResult, error) {
	c.calls++
	if c.failWith != nil {
		return nil, c.failWith
	}
	return c.results, nil
}

// Gate CLOSED (default): a paid fallback must NOT be called even when the
// primary fails and the paid backend is listed as a fallback.
func TestPaidFallbackGate_ClosedByDefault_SkipsPaid(t *testing.T) {
	mgr := NewManager()
	primary := &countingBackend{name: "searxng", tier: CostTierSelfHosted, failWith: fmt.Errorf("primary down")}
	paid := &countingBackend{name: "tavily", tier: CostTierPaid, results: []SearchResult{{Title: "paid"}}}
	mgr.Register(primary)
	mgr.Register(paid)
	mgr.SetPrimary("searxng")
	mgr.SetFallbacks([]string{"tavily"})
	// allowPaidFallback defaults to false (no SetAllowPaidFallback call).

	outcome, err := mgr.Search(SearchOptions{Query: "test"})
	if err == nil {
		t.Fatalf("expected failure when only fallback is paid and gate is closed")
	}
	if paid.calls != 0 {
		t.Errorf("paid backend was called %d times; want 0 (gate closed)", paid.calls)
	}
	foundWarn := false
	for _, w := range outcome.Warnings {
		if strings.Contains(w, "skipped paid fallback") && strings.Contains(w, "tavily") {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Errorf("expected a paid-skip warning, got warnings=%v", outcome.Warnings)
	}
}

// Gate explicitly closed via SetAllowPaidFallback(false): same as default.
func TestPaidFallbackGate_ExplicitFalse_SkipsPaid(t *testing.T) {
	mgr := NewManager()
	primary := &countingBackend{name: "searxng", tier: CostTierSelfHosted, failWith: fmt.Errorf("primary down")}
	paid := &countingBackend{name: "tavily", tier: CostTierPaid, results: []SearchResult{{Title: "paid"}}}
	mgr.Register(primary)
	mgr.Register(paid)
	mgr.SetPrimary("searxng")
	mgr.SetFallbacks([]string{"tavily"})
	mgr.SetAllowPaidFallback(false)

	if _, err := mgr.Search(SearchOptions{Query: "test"}); err == nil {
		t.Fatalf("expected failure with gate closed")
	}
	if paid.calls != 0 {
		t.Errorf("paid backend called %d times; want 0", paid.calls)
	}
}

// Gate OPEN: the paid fallback IS used and serves the request.
func TestPaidFallbackGate_Open_UsesPaid(t *testing.T) {
	mgr := NewManager()
	primary := &countingBackend{name: "searxng", tier: CostTierSelfHosted, failWith: fmt.Errorf("primary down")}
	paid := &countingBackend{name: "tavily", tier: CostTierPaid, results: []SearchResult{{Title: "paid result", URL: "https://t.example"}}}
	mgr.Register(primary)
	mgr.Register(paid)
	mgr.SetPrimary("searxng")
	mgr.SetFallbacks([]string{"tavily"})
	mgr.SetAllowPaidFallback(true)

	outcome, err := mgr.Search(SearchOptions{Query: "test"})
	if err != nil {
		t.Fatalf("expected success with gate open: %v", err)
	}
	if paid.calls != 1 {
		t.Errorf("paid backend called %d times; want 1 (gate open)", paid.calls)
	}
	if outcome.Backend != "tavily" || !outcome.FallbackUsed {
		t.Errorf("expected fallback to tavily, got backend=%q fallbackUsed=%v", outcome.Backend, outcome.FallbackUsed)
	}
	if len(outcome.Results) != 1 || outcome.Results[0].Title != "paid result" {
		t.Errorf("unexpected results: %v", outcome.Results)
	}
}

// Free-external fallback is NOT gated: it should be used even when the gate is
// closed (only paid backends are blocked).
func TestPaidFallbackGate_FreeExternalNotGated(t *testing.T) {
	mgr := NewManager()
	primary := &countingBackend{name: "searxng", tier: CostTierSelfHosted, failWith: fmt.Errorf("primary down")}
	free := &countingBackend{name: "jina", tier: CostTierFreeExternal, results: []SearchResult{{Title: "free"}}}
	mgr.Register(primary)
	mgr.Register(free)
	mgr.SetPrimary("searxng")
	mgr.SetFallbacks([]string{"jina"})
	// gate closed

	outcome, err := mgr.Search(SearchOptions{Query: "test"})
	if err != nil {
		t.Fatalf("free_external fallback should succeed even with gate closed: %v", err)
	}
	if free.calls != 1 {
		t.Errorf("free backend called %d times; want 1", free.calls)
	}
	if outcome.Backend != "jina" {
		t.Errorf("expected jina, got %q", outcome.Backend)
	}
}

// --- fallback DEFAULT OFF: no fallbacks configured -> nothing else tried -----

func TestFallback_DefaultOff_NoFallbackTried(t *testing.T) {
	mgr := NewManager()
	primary := &countingBackend{name: "searxng", tier: CostTierSelfHosted, failWith: fmt.Errorf("primary down")}
	// A backend that, if ever called as a fallback, would record a call.
	never := &countingBackend{name: "tavily", tier: CostTierPaid, results: []SearchResult{{Title: "should not appear"}}}
	mgr.Register(primary)
	mgr.Register(never)
	mgr.SetPrimary("searxng")
	// SetFallbacks NOT called -> empty fallback chain (default off).

	_, err := mgr.Search(SearchOptions{Query: "test"})
	if err == nil {
		t.Fatalf("expected primary failure to surface (no fallbacks)")
	}
	if never.calls != 0 {
		t.Errorf("non-fallback backend was called %d times; want 0", never.calls)
	}
}

// --- aggregateError preserves the primary's typed code via errors.As ---------

func TestAggregateError_PreservesPrimaryCode(t *testing.T) {
	mgr := NewManager()
	primary := &countingBackend{
		name: "searxng", tier: CostTierSelfHosted,
		failWith: &BackendError{Backend: "searxng", Err: fmt.Errorf("dns fail"), Code: ErrCodeNetwork},
	}
	mgr.Register(primary)
	mgr.SetPrimary("searxng")

	_, err := mgr.Search(SearchOptions{Query: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	var be *BackendError
	if !errors.As(err, &be) {
		t.Fatalf("expected to recover *BackendError via errors.As, got %T: %v", err, err)
	}
	if be.Code != ErrCodeNetwork {
		t.Errorf("recovered code = %d, want ErrCodeNetwork(%d)", be.Code, ErrCodeNetwork)
	}
}

// --- End-to-end: paid backend via base_url -> mock counts hits ---------------

// A paid (Tavily) backend pointed at a counting httptest server must receive
// ZERO requests when used purely as a closed-gate fallback through the Manager.
func TestPaidFallbackGate_RealTavilyMock_ZeroHits(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"x","url":"https://x"}]}`)
	}))
	defer srv.Close()

	tavily := NewTavilyBackend("fake-key", time.Second, "basic", false, false)
	tavily.BaseURL = srv.URL // point paid backend at the counting mock

	primary := &countingBackend{name: "searxng", tier: CostTierSelfHosted, failWith: fmt.Errorf("primary down")}

	mgr := NewManager()
	mgr.Register(primary)
	mgr.Register(tavily)
	mgr.SetPrimary("searxng")
	mgr.SetFallbacks([]string{"tavily"})
	// gate closed (default)

	if _, err := mgr.Search(SearchOptions{Query: "test"}); err == nil {
		t.Fatal("expected failure (paid fallback skipped)")
	}
	if hits != 0 {
		t.Fatalf("tavily mock received %d requests; want 0 (paid gate closed)", hits)
	}
}

// With the gate OPEN, the same Tavily-via-mock IS hit exactly once.
func TestPaidFallbackGate_RealTavilyMock_OneHitWhenOpen(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"x","url":"https://x"}]}`)
	}))
	defer srv.Close()

	tavily := NewTavilyBackend("fake-key", time.Second, "basic", false, false)
	tavily.BaseURL = srv.URL

	primary := &countingBackend{name: "searxng", tier: CostTierSelfHosted, failWith: fmt.Errorf("primary down")}

	mgr := NewManager()
	mgr.Register(primary)
	mgr.Register(tavily)
	mgr.SetPrimary("searxng")
	mgr.SetFallbacks([]string{"tavily"})
	mgr.SetAllowPaidFallback(true) // gate open

	outcome, err := mgr.Search(SearchOptions{Query: "test"})
	if err != nil {
		t.Fatalf("expected success via tavily mock: %v", err)
	}
	if hits != 1 {
		t.Fatalf("tavily mock received %d requests; want 1 (gate open)", hits)
	}
	if outcome.Backend != "tavily" {
		t.Errorf("expected backend tavily, got %q", outcome.Backend)
	}
}
