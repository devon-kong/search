package backends

import (
	"net/url"
	"regexp"
	"strings"
)

// maxRedactedBodyLen bounds raw HTTP body echoed into error messages so we do
// not leak large or sensitive response payloads.
const maxRedactedBodyLen = 256

// redactedPlaceholder replaces any matched secret value.
const redactedPlaceholder = "[REDACTED]"

// Regexes for header-style secrets that may appear in wrapped error strings or
// debug output. These match "Header: value" and "Bearer <token>" forms.
var (
	bearerRe = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`)

	// Sensitive header names whose values must be masked when rendered as
	// "name: value" or "name=value".
	sensitiveHeaderRe = regexp.MustCompile(`(?i)(authorization|x-subscription-token|x-api-key|api[_-]?key)\s*[:=]\s*\S+`)
)

// RedactURL removes userinfo (basic-auth user:pass@) from a URL so credentials
// embedded in searxng_url and similar are never echoed. Non-URL strings are
// returned unchanged except for inline credential patterns.
func RedactURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	if u, err := url.Parse(trimmed); err == nil && u.User != nil {
		u.User = url.User(redactedPlaceholder)
		return u.String()
	}
	return raw
}

// RedactSecrets masks credentials that may appear anywhere in a string:
// URL userinfo, Authorization/Bearer tokens, and known secret header values.
// It is safe to apply to any text destined for stdout, stderr, or an error
// message envelope.
func RedactSecrets(s string) string {
	if s == "" {
		return s
	}
	out := s

	// Mask userinfo inside any embedded URLs (scheme://user:pass@host...).
	out = redactURLUserinfo(out)

	// Mask "Bearer <token>" sequences.
	out = bearerRe.ReplaceAllString(out, "Bearer "+redactedPlaceholder)

	// Mask sensitive "header: value" / "header=value" pairs.
	out = sensitiveHeaderRe.ReplaceAllStringFunc(out, func(m string) string {
		sep := ":"
		if strings.Contains(m, "=") && !strings.Contains(m, ":") {
			sep = "="
		}
		idx := strings.IndexAny(m, ":=")
		if idx < 0 {
			return m
		}
		return strings.TrimSpace(m[:idx]) + sep + " " + redactedPlaceholder
	})

	return out
}

// urlUserinfoRe matches the userinfo portion of an embedded URL.
var urlUserinfoRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)([^/@\s]+)@`)

func redactURLUserinfo(s string) string {
	return urlUserinfoRe.ReplaceAllString(s, "${1}"+redactedPlaceholder+"@")
}

// TruncateBody bounds and redacts a raw HTTP response body for inclusion in an
// error message. It strips secrets and caps length.
func TruncateBody(body string) string {
	clean := RedactSecrets(strings.TrimSpace(body))
	if len(clean) > maxRedactedBodyLen {
		return clean[:maxRedactedBodyLen] + "...(truncated)"
	}
	return clean
}
