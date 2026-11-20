// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0
//go:build !windows

package pathutil

import "testing"

func TestUnixURIConversions(t *testing.T) {
	for _, tt := range []struct{ path, uri string }{{"/", "file:///"}, {"/a b/../space #%.yaml", "file:///space%20%23%25.yaml"}, {`/literal\name?.yaml`, "file:///literal%5Cname%3F.yaml"}} {
		u, err := ToFileURI(tt.path)
		if err != nil || u != tt.uri {
			t.Errorf("%q: %q %v", tt.path, u, err)
		}
	}
	if _, err := FromFileURI("file://server/share/file.yaml"); err == nil {
		t.Fatal("remote host became local Unix path")
	}
	if SamePath("/Case", "/case") {
		t.Fatal("Unix lexical identity lost casing")
	}
}
