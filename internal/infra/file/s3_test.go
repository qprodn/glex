package fileinfra

import "testing"

func TestMimeAllowed(t *testing.T) {
	allow := []string{"image/*", "application/pdf", "text/plain"}
	cases := []struct {
		mime string
		want bool
	}{
		{"image/png", true},
		{"image/", true},
		{"application/pdf", true},
		{"text/plain", true},
		{"application/x-sh", false},
		{"imagefoo/png", false}, // "image/" prefix does not match "imagefoo/"
		{"", false},
		{"text/plain; charset=utf-8", true}, // DetectContentType parameterizes text types; parameters are stripped before matching
	}
	for _, c := range cases {
		if got := mimeAllowed(c.mime, allow); got != c.want {
			t.Errorf("mimeAllowed(%q) = %v, want %v", c.mime, got, c.want)
		}
	}
}

func TestDefaultAllowMIMEEntriesAreWellFormed(t *testing.T) {
	for _, entry := range DefaultAllowMIME {
		if entry == "" {
			t.Errorf("empty whitelist entry")
			continue
		}
		if !mimeAllowed(entry, DefaultAllowMIME) {
			t.Errorf("default entry %q does not admit itself", entry)
		}
	}
}
