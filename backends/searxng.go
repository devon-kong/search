package backends

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SearxngBackend implements SearchBackend for SearXNG instances
type SearxngBackend struct {
	BaseURL     string
	Username    string
	Password    string
	HTTPMethod  string
	Timeout     time.Duration
	NoVerifySSL bool
	NoUserAgent bool
	client      *http.Client
}

// SearxngRawResponse carries the untouched SearXNG JSON body plus the parsed
// fields sx needs for normal search output and diagnostics.
type SearxngRawResponse struct {
	Raw         json.RawMessage
	Results     []SearchResult
	Diagnostics SearxngDiagnostics
}

// SearxngDiagnostics is intentionally conservative: version-sensitive SearXNG
// arrays are preserved as raw JSON while stable scalar metadata is typed.
type SearxngDiagnostics struct {
	Answers               json.RawMessage `json:"answers"`
	Suggestions           json.RawMessage `json:"suggestions"`
	Infoboxes             json.RawMessage `json:"infoboxes"`
	UnresponsiveEngines   json.RawMessage `json:"unresponsive_engines"`
	NumberOfResults       int             `json:"number_of_results"`
	StrictEnginesWarnings []string        `json:"strict_engines_warnings,omitempty"`
}

// NewSearxngBackend creates a new SearXNG backend
func NewSearxngBackend(baseURL, username, password, httpMethod string, timeout time.Duration, noVerifySSL, noUserAgent bool) *SearxngBackend {
	client := &http.Client{
		Timeout: timeout,
	}

	if noVerifySSL {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client.Transport = tr
	}

	return &SearxngBackend{
		BaseURL:     baseURL,
		Username:    username,
		Password:    password,
		HTTPMethod:  strings.ToUpper(httpMethod),
		Timeout:     timeout,
		NoVerifySSL: noVerifySSL,
		NoUserAgent: noUserAgent,
		client:      client,
	}
}

// Name returns the backend identifier
func (s *SearxngBackend) Name() string {
	return "searxng"
}

// CostTier reports SearXNG as self-hosted (no per-request paid cost).
func (s *SearxngBackend) CostTier() string {
	return CostTierSelfHosted
}

// IsAvailable checks if SearXNG is configured and reachable
func (s *SearxngBackend) IsAvailable() bool {
	if s.BaseURL == "" {
		return false
	}

	// Try a simple health check or just validate URL is parseable
	u, err := url.Parse(s.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}

	return true
}

// Search performs a search against SearXNG
func (s *SearxngBackend) Search(opts SearchOptions) ([]SearchResult, error) {
	raw, err := s.SearchRaw(opts)
	if err != nil {
		return nil, err
	}
	return raw.Results, nil
}

// SearchRaw performs a search and returns SearXNG's original JSON body along
// with parsed results and diagnostics.
func (s *SearxngBackend) SearchRaw(opts SearchOptions) (SearxngRawResponse, error) {
	var out SearxngRawResponse

	if !s.IsAvailable() {
		return out, &BackendError{
			Backend: s.Name(),
			Err:     fmt.Errorf("SearXNG URL not configured"),
			Code:    ErrCodeUnavailable,
		}
	}

	query := opts.Query
	if opts.Site != "" {
		query = fmt.Sprintf("site:%s %s", opts.Site, query)
	}

	var searchURL string
	var reqBody io.Reader

	if s.HTTPMethod == "POST" {
		searchURL = fmt.Sprintf("%s/search", s.BaseURL)
		data := s.buildParams(query, opts)
		reqBody = strings.NewReader(data.Encode())
	} else {
		u, err := url.Parse(s.BaseURL + "/search")
		if err != nil {
			return out, &BackendError{
				Backend: s.Name(),
				Err:     fmt.Errorf("invalid SearXNG URL: %s", RedactSecrets(err.Error())),
				Code:    ErrCodeInvalidResponse,
			}
		}
		u.RawQuery = s.buildParams(query, opts).Encode()
		searchURL = u.String()
	}

	var req *http.Request
	var err error

	if s.HTTPMethod == "POST" {
		req, err = http.NewRequest("POST", searchURL, reqBody)
		if err != nil {
			return out, s.wrapError(err, ErrCodeNetwork)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req, err = http.NewRequest("GET", searchURL, nil)
		if err != nil {
			return out, s.wrapError(err, ErrCodeNetwork)
		}
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	if !s.NoUserAgent {
		req.Header.Set("User-Agent", "sx/2.0")
	}

	if s.Username != "" && s.Password != "" {
		req.SetBasicAuth(s.Username, s.Password)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return out, s.wrapError(err, ErrCodeNetwork)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return out, &BackendError{
			Backend: s.Name(),
			Err:     fmt.Errorf("HTTP %d: %s", resp.StatusCode, TruncateBody(string(body))),
			Code:    resp.StatusCode,
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, s.wrapError(err, ErrCodeInvalidResponse)
	}

	var searchResp SearxngResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return out, s.wrapError(fmt.Errorf("failed to parse JSON: %v", err), ErrCodeInvalidResponse)
	}

	// Transform SearxngResponse to []SearchResult
	results := make([]SearchResult, len(searchResp.Results))
	for i, r := range searchResp.Results {
		results[i] = SearchResult(r)
	}

	out.Raw = append(json.RawMessage(nil), body...)
	out.Results = results
	out.Diagnostics = searchResp.Diagnostics()
	return out, nil
}

// SearxngConfigResponse is the subset of SearXNG's /config response that the
// engine inventory needs. Unknown fields are ignored.
type SearxngConfigResponse struct {
	Engines []SearxngEngineConfig `json:"engines"`
}

// SearxngEngineConfig describes one upstream engine from SearXNG /config.
type SearxngEngineConfig struct {
	Name             string   `json:"name"`
	Shortcut         string   `json:"shortcut"`
	Categories       []string `json:"categories"`
	Enabled          bool     `json:"enabled"`
	Timeout          float64  `json:"timeout"`
	Paging           bool     `json:"paging"`
	SafeSearch       bool     `json:"safesearch"`
	TimeRangeSupport bool     `json:"time_range_support"`
	LanguageSupport  bool     `json:"language_support"`
}

// SearxngConfigFetcher is an optional extension implemented by SearXNG backends
// that can return the upstream engine inventory from /config. The base
// SearchBackend interface is unchanged.
type SearxngConfigFetcher interface {
	FetchConfig() (SearxngConfigResponse, error)
}

// FetchConfig fetches the upstream engine inventory from SearXNG's /config
// endpoint. It always issues a single GET (no /search, no query params) and
// never spends paid credits.
func (s *SearxngBackend) FetchConfig() (SearxngConfigResponse, error) {
	var out SearxngConfigResponse

	if !s.IsAvailable() {
		return out, &BackendError{
			Backend: s.Name(),
			Err:     fmt.Errorf("SearXNG URL not configured"),
			Code:    ErrCodeUnavailable,
		}
	}

	u, err := url.Parse(s.BaseURL + "/config")
	if err != nil {
		return out, &BackendError{
			Backend: s.Name(),
			Err:     fmt.Errorf("invalid SearXNG URL: %s", RedactSecrets(err.Error())),
			Code:    ErrCodeInvalidResponse,
		}
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return out, s.wrapError(err, ErrCodeNetwork)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	if !s.NoUserAgent {
		req.Header.Set("User-Agent", "sx/2.0")
	}

	if s.Username != "" && s.Password != "" {
		req.SetBasicAuth(s.Username, s.Password)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return out, s.wrapError(err, ErrCodeNetwork)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return out, &BackendError{
			Backend: s.Name(),
			Err:     fmt.Errorf("HTTP %d: %s", resp.StatusCode, TruncateBody(string(body))),
			Code:    resp.StatusCode,
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, s.wrapError(err, ErrCodeInvalidResponse)
	}

	if err := json.Unmarshal(body, &out); err != nil {
		return out, s.wrapError(fmt.Errorf("failed to parse JSON: %v", err), ErrCodeInvalidResponse)
	}

	return out, nil
}

// buildParams constructs URL parameters for SearXNG
func (s *SearxngBackend) buildParams(query string, opts SearchOptions) url.Values {
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")

	if len(opts.Categories) > 0 {
		normalized := make([]string, len(opts.Categories))
		for i, cat := range opts.Categories {
			normalized[i] = normalizeCategory(cat)
		}
		params.Set("categories", strings.Join(normalized, ","))
	}

	if len(opts.Engines) > 0 {
		params.Set("engines", strings.Join(opts.Engines, ","))
	}

	if opts.Language != "" {
		params.Set("language", opts.Language)
	}

	if opts.SafeSearch != "" {
		if val, ok := safeSearchOptions[opts.SafeSearch]; ok {
			params.Set("safesearch", strconv.Itoa(val))
		}
	}

	if opts.TimeRange != "" {
		params.Set("time_range", opts.TimeRange)
	}

	if opts.PageNo > 1 {
		params.Set("pageno", strconv.Itoa(opts.PageNo))
	}

	if opts.NumResults > 0 {
		params.Set("num", strconv.Itoa(opts.NumResults))
	}

	return params
}

func (s *SearxngBackend) wrapError(err error, code int) *BackendError {
	// Redact any credentials (e.g. user:pass@ embedded in searxng_url) that may
	// appear in network/parse errors before they reach output.
	return &BackendError{
		Backend: s.Name(),
		Err:     fmt.Errorf("%s", RedactSecrets(err.Error())),
		Code:    code,
	}
}

// Internal response type for parsing SearXNG JSON
type SearxngResponse struct {
	Results             []searxngResult `json:"results"`
	Answers             json.RawMessage `json:"answers"`
	Suggestions         json.RawMessage `json:"suggestions"`
	Infoboxes           json.RawMessage `json:"infoboxes"`
	UnresponsiveEngines json.RawMessage `json:"unresponsive_engines"`
	NumberOfResults     int             `json:"number_of_results"`
}

type searxngResult SearchResult

// Diagnostics returns a stable diagnostics object even when a SearXNG instance
// omits optional fields.
func (r SearxngResponse) Diagnostics() SearxngDiagnostics {
	return SearxngDiagnostics{
		Answers:             normalizeRawJSONArray(r.Answers),
		Suggestions:         normalizeRawJSONArray(r.Suggestions),
		Infoboxes:           normalizeRawJSONArray(r.Infoboxes),
		UnresponsiveEngines: normalizeRawJSONArray(r.UnresponsiveEngines),
		NumberOfResults:     r.NumberOfResults,
	}
}

func normalizeRawJSONArray(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("[]")
	}
	return append(json.RawMessage(nil), raw...)
}

var safeSearchOptions = map[string]int{
	"none":     0,
	"moderate": 1,
	"strict":   2,
}

// normalizeCategory converts category aliases to canonical form
func normalizeCategory(category string) string {
	aliases := map[string]string{
		"social+media": "social media",
		"social-media": "social media",
		"social_media": "social media",
		"socialmedia":  "social media",
	}
	if canonical, ok := aliases[category]; ok {
		return canonical
	}
	return category
}
