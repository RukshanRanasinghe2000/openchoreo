// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/openchoreo/openchoreo/tools/lint/ruleengine/template"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestUnavailableInitializationTemplateDoesNotFallBack(t *testing.T) {
	previous := template.CurrentVersion()
	t.Cleanup(func() { _ = template.SetVersion(previous) })
	s := NewServer()
	_, err := s.Initialize(nil, &protocol.InitializeParams{InitializationOptions: map[string]any{"templateVersion": "v0.0.1"}})
	if err == nil {
		t.Fatal("explicit unavailable version must fail initialization")
	}
	if template.CurrentVersion() != previous {
		t.Fatal("unavailable request switched the active template")
	}
}
