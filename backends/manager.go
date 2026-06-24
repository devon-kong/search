package backends

import (
	"fmt"
	"strings"
)

// Manager coordinates search across multiple backends with fallback support
type Manager struct {
	primary           SearchBackend
	fallbacks         []SearchBackend
	registry          map[string]SearchBackend
	allowPaidFallback bool
}

// SearchOutcome carries the results of a Search along with metadata about which
// backend served the request and whether a fallback was used.
type SearchOutcome struct {
	Results        []SearchResult
	Backend        string   // name of the backend that produced Results
	FallbackUsed   bool     // true if a fallback (not the primary) served the request
	FallbackReason string   // human-readable reason describing fallback/skip decisions
	Warnings       []string // non-fatal notes (e.g. paid backends skipped)
	Diagnostics    *SearxngDiagnostics
}

// NewManager creates a new backend manager
func NewManager() *Manager {
	return &Manager{
		registry: make(map[string]SearchBackend),
	}
}

// SetAllowPaidFallback controls whether automatic fallback may use paid
// backends (CostTierPaid). When false (default), paid backends are skipped in
// the fallback chain and a warning is recorded.
func (m *Manager) SetAllowPaidFallback(allow bool) {
	m.allowPaidFallback = allow
}

// Register adds a backend to the registry
func (m *Manager) Register(backend SearchBackend) {
	m.registry[backend.Name()] = backend
}

// SetPrimary sets the primary search backend by name
func (m *Manager) SetPrimary(name string) error {
	backend, ok := m.registry[name]
	if !ok {
		return fmt.Errorf("unknown backend: %s (available: %s)", name, m.availableNames())
	}
	m.primary = backend
	return nil
}

// SetFallbacks sets the fallback backends in order
func (m *Manager) SetFallbacks(names []string) error {
	m.fallbacks = nil
	for _, name := range names {
		backend, ok := m.registry[name]
		if !ok {
			return fmt.Errorf("unknown fallback backend: %s (available: %s)", name, m.availableNames())
		}
		m.fallbacks = append(m.fallbacks, backend)
	}
	return nil
}

// Search performs a search using the primary backend, falling back to
// alternatives. It returns a SearchOutcome with the results plus metadata about
// which backend served the request and whether a fallback was used.
//
// Automatic fallback never uses a paid backend (CostTierPaid) unless
// SetAllowPaidFallback(true) was called; skipped paid backends are recorded in
// the outcome's Warnings and FallbackReason so callers can surface them.
func (m *Manager) Search(opts SearchOptions) (SearchOutcome, error) {
	if m.primary == nil {
		return SearchOutcome{}, fmt.Errorf("no primary backend configured")
	}

	// Try primary backend first
	results, diagnostics, err := searchBackendWithDiagnostics(m.primary, opts)
	if err == nil {
		return SearchOutcome{
			Results:     results,
			Backend:     m.primary.Name(),
			Diagnostics: diagnostics,
		}, nil
	}

	// Primary failed - collect errors and warnings
	errors := []string{err.Error()}
	var warnings []string

	// Try fallbacks in order
	for _, fb := range m.fallbacks {
		// Gate: never auto-fallback to a paid backend unless explicitly allowed.
		if fb.CostTier() == CostTierPaid && !m.allowPaidFallback {
			warnings = append(warnings, fmt.Sprintf("skipped paid fallback %q (set allow_paid_fallback=true to enable)", fb.Name()))
			errors = append(errors, fmt.Sprintf("%s: skipped (paid backend, allow_paid_fallback=false)", fb.Name()))
			continue
		}

		if !fb.IsAvailable() {
			errors = append(errors, fmt.Sprintf("%s: not configured", fb.Name()))
			continue
		}

		results, diagnostics, fbErr := searchBackendWithDiagnostics(fb, opts)
		if fbErr == nil {
			return SearchOutcome{
				Results:        results,
				Backend:        fb.Name(),
				FallbackUsed:   true,
				FallbackReason: fmt.Sprintf("primary %q failed: %v", m.primary.Name(), err),
				Warnings:       warnings,
				Diagnostics:    diagnostics,
			}, nil
		}
		errors = append(errors, fbErr.Error())
	}

	// Wrap the primary error so callers (e.g. the JSON envelope) can still
	// recover the typed *BackendError code/retryable classification via
	// errors.As, while the message lists every backend that was tried.
	return SearchOutcome{Warnings: warnings}, &aggregateError{
		primary: err,
		summary: fmt.Sprintf("all backends failed:\n  %s", strings.Join(errors, "\n  ")),
	}
}

// aggregateError reports that all backends failed while preserving the primary
// backend's underlying error for type inspection (errors.As / errors.Unwrap).
type aggregateError struct {
	primary error
	summary string
}

func (a *aggregateError) Error() string { return a.summary }
func (a *aggregateError) Unwrap() error { return a.primary }

// SearchExplicit searches using a specific backend by name (no fallback)
func (m *Manager) SearchExplicit(name string, opts SearchOptions) ([]SearchResult, error) {
	outcome, err := m.SearchExplicitOutcome(name, opts)
	if err != nil {
		return nil, err
	}
	return outcome.Results, nil
}

// SearchExplicitOutcome searches using a specific backend by name (no fallback)
// and returns backend metadata plus any SearXNG diagnostics.
func (m *Manager) SearchExplicitOutcome(name string, opts SearchOptions) (SearchOutcome, error) {
	backend, ok := m.registry[name]
	if !ok {
		return SearchOutcome{}, fmt.Errorf("unknown backend: %s (available: %s)", name, m.availableNames())
	}
	if !backend.IsAvailable() {
		return SearchOutcome{}, fmt.Errorf("backend %s is not configured (missing API key?)", name)
	}
	results, diagnostics, err := searchBackendWithDiagnostics(backend, opts)
	if err != nil {
		return SearchOutcome{}, err
	}
	return SearchOutcome{
		Results:     results,
		Backend:     backend.Name(),
		Diagnostics: diagnostics,
	}, nil
}

// GetBackend returns a backend by name
func (m *Manager) GetBackend(name string) (SearchBackend, bool) {
	b, ok := m.registry[name]
	return b, ok
}

// AvailableBackends returns names of all registered backends
func (m *Manager) AvailableBackends() []string {
	names := make([]string, 0, len(m.registry))
	for name := range m.registry {
		names = append(names, name)
	}
	return names
}

// ConfiguredBackends returns names of backends that are available (configured)
func (m *Manager) ConfiguredBackends() []string {
	names := make([]string, 0, len(m.registry))
	for name, backend := range m.registry {
		if backend.IsAvailable() {
			names = append(names, name)
		}
	}
	return names
}

func (m *Manager) availableNames() string {
	return strings.Join(m.AvailableBackends(), ", ")
}

type searxngRawSearcher interface {
	SearchRaw(opts SearchOptions) (SearxngRawResponse, error)
}

func searchBackendWithDiagnostics(backend SearchBackend, opts SearchOptions) ([]SearchResult, *SearxngDiagnostics, error) {
	if rawBackend, ok := backend.(searxngRawSearcher); ok {
		raw, err := rawBackend.SearchRaw(opts)
		if err != nil {
			return nil, nil, err
		}
		diagnostics := raw.Diagnostics
		return raw.Results, &diagnostics, nil
	}

	results, err := backend.Search(opts)
	return results, nil, err
}
