// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseFile reads the file at path and dispatches to ParseJSON or ParseCSV
// based on the extension (§4.1).
//
// Errors from missing files and I/O bubble up untouched (so CLI layers can
// surface the OS message directly). Validation and schema errors carry
// the file path.
func ParseFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return ParseJSON(data, path)
	case ".csv":
		return ParseCSV(data, path)
	}
	return nil, fmt.Errorf("%s: unrecognized manifest extension (expected .json or .csv)", path)
}
