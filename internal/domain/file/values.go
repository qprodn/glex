package file

import (
	"regexp"
	"strings"
)

// BizPathPattern is the charset a business directory prefix must match:
// slash-separated segments of lowercase alphanumerics and hyphens, no
// leading or trailing slash — "..", absolute paths, and traversal shapes are
// unrepresentable (go-wind's directory safety rule).
var BizPathPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(/[a-z0-9][a-z0-9-]*)*$`)

const (
	// MaxOriginalNameLength bounds the stored original file name.
	MaxOriginalNameLength = 256
	// MaxSuffixLength bounds the normalized suffix.
	MaxSuffixLength = 16
	// MaxBizPathLength bounds the business directory prefix.
	MaxBizPathLength = 128
)

// NormalizeSuffix derives the stored suffix from an original file name: the
// last dot-separated segment, lowercased, kept only when it is alphanumeric
// and short enough ("archive.tar.gz" → "gz", "报告.PDF" → "pdf", "noext" → "").
func NormalizeSuffix(originalName string) string {
	dot := strings.LastIndexByte(originalName, '.')
	if dot < 0 || dot == len(originalName)-1 {
		return ""
	}
	suffix := strings.ToLower(originalName[dot+1:])
	if len(suffix) > MaxSuffixLength {
		return ""
	}
	for _, r := range suffix {
		if !('a' <= r && r <= 'z' || '0' <= r && r <= '9') {
			return ""
		}
	}
	return suffix
}
