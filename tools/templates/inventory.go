// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
)

// Inventory describes only compiled-in assets, never on-disk overrides.
type Inventory struct {
	Version  string            `json:"version"`
	Latest   string            `json:"latest"`
	Versions []string          `json:"versions"`
	Files    map[string]string `json:"files"`
}

// EmbeddedInventory lets release tooling verify the actual executable's assets.
func EmbeddedInventory(version string) (Inventory, error) {
	var result Inventory
	manifest, err := FS.ReadFile("template-version.json")
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(manifest, &result); err != nil {
		return result, err
	}
	result.Version = version
	result.Files = make(map[string]string)
	err = fs.WalkDir(FS, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := FS.ReadFile(name)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		result.Files[name] = hex.EncodeToString(digest[:])
		return nil
	})
	return result, err
}
