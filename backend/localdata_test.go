// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os"
	"path/filepath"
)

// localDataPath builds a path under the developer's local audio asset dir.
// Real-data tests use it for IR/VDC files that are private assets, never in
// the repo (every caller skips when the file is absent). $HOME keeps the
// path portable across machines (the release gate forbids home-dir literals).
func localDataPath(parts ...string) string {
	segs := append([]string{os.Getenv("HOME"), ".config", "jamesdsp"}, parts...)
	return filepath.Join(segs...)
}
