package main

import "testing"

// TestResolveURLHandler covers the url_handler resolution used by openURL: an
// explicit config value wins, otherwise the platform default applies, and an
// unknown platform with no config yields an empty (unsupported) handler.
func TestResolveURLHandler(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		goos       string
		want       string
	}{
		{"empty config falls back to darwin default", "", "darwin", "open"},
		{"empty config falls back to linux default", "", "linux", "xdg-open"},
		{"empty config falls back to windows default", "", "windows", "explorer"},
		{"explicit config overrides platform default", "firefox", "darwin", "firefox"},
		{"whitespace-only config is treated as empty", "   ", "linux", "xdg-open"},
		{"explicit config is trimmed", "  brave  ", "linux", "brave"},
		{"unknown platform with no config is unsupported", "", "plan9", ""},
		{"unknown platform still honors explicit config", "myhandler", "plan9", "myhandler"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveURLHandler(tt.configured, tt.goos); got != tt.want {
				t.Errorf("resolveURLHandler(%q, %q) = %q, want %q", tt.configured, tt.goos, got, tt.want)
			}
		})
	}
}
