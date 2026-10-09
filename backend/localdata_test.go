// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os"
	"path/filepath"
)

func localDataPath(parts ...string) string {
	segs := append([]string{os.Getenv("HOME"), ".config", "jamesdsp"}, parts...)
	return filepath.Join(segs...)
}
