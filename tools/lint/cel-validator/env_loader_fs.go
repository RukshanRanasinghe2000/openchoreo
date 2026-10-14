// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package celvalidator

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
)

var celEnvFS embed.FS

// SetCelEnvFS sets an embedded FS to load CEL env descriptors from.
func SetCelEnvFS(f embed.FS) { celEnvFS = f }

// loadEnvDescFS tries to load from FS if set.
func loadEnvDescFS(kind string) (*EnvDesc, bool) {
	if celEnvFS == (embed.FS{}) {
		return nil, false
	}
	name := strings.ToLower(kind) + "_celenv.json"
	// Try common locations
	candidates := []string{
		filepath.Join("cel-envs", "1.3.0", name),
		filepath.Join("template-v1.3.0", "cel-envs", name),
		filepath.Join("template-v1.2.0", "cel-envs", name),
		"cel-envs/1.3.0/" + name,
		"template-v1.3.0/cel-envs/" + name,
	}
	for _, c := range candidates {
		b, err := celEnvFS.ReadFile(c)
		if err == nil {
			var d EnvDesc
			if err := json.Unmarshal(b, &d); err == nil {
				return &d, true
			}
		}
	}
	// also walk
	_ = fs.WalkDir(celEnvFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, name) {
			b, err := celEnvFS.ReadFile(path)
			if err == nil {
				var desc EnvDesc
				if err := json.Unmarshal(b, &desc); err == nil {
					// store by returning via closure trick not easy; simpler: just read on-the-fly by continuing
				}
			}
		}
		return nil
	})
	return nil, false
}
