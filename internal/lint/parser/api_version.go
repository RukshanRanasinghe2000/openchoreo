// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package parser

import "strings"

// OpenChoreoAPIPrefix is the apiVersion group that identifies an OpenChoreo
// YAML document.
const OpenChoreoAPIPrefix = "openchoreo.dev/"

// IsOpenChoreoAPIVersion reports whether the given apiVersion value belongs to
// the OpenChoreo group (openchoreo.dev, any version).
func IsOpenChoreoAPIVersion(apiVersion string) bool {
	return apiVersion == "openchoreo.dev" || strings.HasPrefix(apiVersion, OpenChoreoAPIPrefix)
}

// HasOpenChoreoAPIVersion reports whether the raw YAML text declares an
// OpenChoreo apiVersion (a line `apiVersion: openchoreo.dev/...`). Validation
// only runs on files that pass this check; every other YAML file is ignored.
func HasOpenChoreoAPIVersion(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, "apiVersion:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "apiVersion:"))
		value = strings.Trim(value, `"' `)
		if IsOpenChoreoAPIVersion(value) {
			return true
		}
	}
	return false
}
