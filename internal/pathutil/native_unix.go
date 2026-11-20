// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
//go:build !windows

package pathutil

import (
	"fmt"
	"strings"
)

func encodeNative(path string) (string, string, error) { return "", path, nil }
func decodeNative(host, path string) (string, error) {
	if host != "" && !strings.EqualFold(host, "localhost") {
		return "", fmt.Errorf("nonlocal file URI authority is not a Unix filesystem path")
	}
	return path, nil
}
func nativeKey(path string) string { return path }
