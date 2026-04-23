package purl

import (
	"fmt"
	"strings"
)

// parseSubpath decodes a subpath string, drops empty/"."/".." segments, and
// rejoins with '/'. Segments containing a literal '/' after decoding are an
// error — they would hide path structure under a single segment.
func parseSubpath(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	var cleaned []string
	for _, seg := range strings.Split(raw, "/") {
		seg = percentDecode(seg)
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		if strings.Contains(seg, "/") {
			return "", fmt.Errorf("subpath segment %q contains '/'", seg)
		}
		cleaned = append(cleaned, seg)
	}

	return strings.Join(cleaned, "/"), nil
}
