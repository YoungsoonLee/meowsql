package push

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Fingerprint returns a 16-character hex identifier for a query.
// Whitespace is collapsed so formatting differences don't create new entries.
func Fingerprint(query string) string {
	normalised := strings.Join(strings.Fields(strings.ToLower(query)), " ")
	h := sha256.Sum256([]byte(normalised))
	return fmt.Sprintf("%x", h[:8]) // 64-bit — collision probability negligible at Phase 3a scale
}
