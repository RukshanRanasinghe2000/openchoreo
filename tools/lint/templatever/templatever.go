// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

// Package templatever reads and writes the template-version.json manifest that
// records the template versions shipped by the rule engine (and which one is
// the latest). It is shared by the json-schema-generator (which updates the
// manifest) and the ruleengine/template registry (which consumes it), so the
// registry no longer hard-codes version names.
package templatever

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// File mirrors the on-disk template-version.json shape. Versions use the
// "v1.3.0" naming style (pre-release suffixes stripped).
type File struct {
	// Latest is the template version used by default. Set to the newest
	// version generated.
	Latest string `json:"latest"`
	// Versions lists every available template version, sorted ascending.
	Versions []string `json:"versions"`
}

// Parse decodes a template-version.json document.
func Parse(data []byte) (File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("parse template-version.json: %w", err)
	}
	return f, nil
}

// Marshal encodes the manifest with a stable order (versions sorted
// ascending) and a trailing newline.
func Marshal(f File) ([]byte, error) {
	// Copy protects the caller from mutations to the Versions slice.
	f.Versions = append([]string(nil), f.Versions...)
	sortVersions(f.Versions)
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Compare orders two "vX.Y.Z" version strings the way a user expects, so that
// v1.10.0 sorts after v1.9.0. It returns a negative value when a < b, zero
// when equal, and a positive value when a > b.
func Compare(a, b string) int {
	amaj, amin, apat := split(a)
	bmaj, bmin, bpat := split(b)
	switch {
	case amaj != bmaj:
		return amaj - bmaj
	case amin != bmin:
		return amin - bmin
	default:
		return apat - bpat
	}
}

// split parses major, minor and patch of a "vX.Y.Z" version string. It never
// panics; malformed pieces count as zero.
func split(v string) (major, minor, patch int) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err == nil {
			nums[i] = n
		}
	}
	return nums[0], nums[1], nums[2]
}

// sortVersions sorts a slice of version strings ascending in place.
func sortVersions(versions []string) {
	// insertion sort keeps this dependency-free and is plenty for a handful
	// of versions.
	for i := 1; i < len(versions); i++ {
		for j := i; j > 0 && Compare(versions[j-1], versions[j]) > 0; j-- {
			versions[j-1], versions[j] = versions[j], versions[j-1]
		}
	}
}
