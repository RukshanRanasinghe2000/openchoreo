// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openchoreo/openchoreo/internal/pathutil"
)

const (
	nameKey         = "name"
	textDocumentKey = "textDocument"
	uriKey          = "uri"
	testIdentity    = "test"
	//nolint:misspell // Deliberately invalid YAML field used to test diagnostics.
	invalidField = "enviroment"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type rpcResult struct {
	message rpcMessage
	err     error
}

func readMessages(ctx context.Context, stream io.Reader, responses chan<- rpcResult) {
	reader := bufio.NewReader(stream)
	for {
		message, err := readMessage(reader)
		select {
		case responses <- rpcResult{message, err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func readMessage(reader *bufio.Reader) (rpcMessage, error) {
	var result rpcMessage
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return result, err
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		key, value, _ := strings.Cut(line, ":")
		if strings.EqualFold(key, "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return result, err
			}
		}
	}
	if length < 0 || length > 16*1024*1024 {
		return result, fmt.Errorf("invalid JSON-RPC content length %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return result, err
	}
	err := json.Unmarshal(body, &result)
	return result, err
}

func smokeReleaseBinary(t *testing.T, binary string) {
	t.Helper()
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		var payload any = map[string]any{"items": []any{}}
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			payload = map[string]any{
				"openchoreo_security_enabled": false,
				"authorization_servers":       []string{base},
				"openchoreo_clients":          []map[string]any{{nameKey: "cli", "client_id": testIdentity, "scopes": []string{}}},
			}
		case "/.well-known/openid-configuration":
			payload = map[string]string{
				"issuer": base, "authorization_endpoint": base + "/authorize",
				"token_endpoint": base + "/token", "jwks_uri": base + "/jwks",
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(api.Close)
	home := t.TempDir()
	config := map[string]any{
		"currentContext": testIdentity, "controlplanes": []map[string]string{{nameKey: testIdentity, "url": api.URL}},
		"credentials": []map[string]string{{nameKey: testIdentity, "token": ""}},
		"contexts": []map[string]string{{
			nameKey: testIdentity, "controlplane": testIdentity, "credentials": testIdentity, "namespace": "default",
		}},
	}
	if err := os.Mkdir(filepath.Join(home, ".openchoreo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(home, ".openchoreo", "config"), config); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	//nolint:gosec // Tests intentionally launch the explicitly selected native release executable.
	cmd := exec.CommandContext(ctx, binary, "lsp", "--stdio")
	cmd.Dir = home
	env := map[string]string{"HOME": home, "USERPROFILE": home}
	cmd.Env = commandEnv(env)
	var stderr lockedBuffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	responses := make(chan rpcResult, 16)
	go readMessages(ctx, stdout, responses)
	send := func(method string, params any, id int) {
		t.Helper()
		message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != 0 {
			message["id"] = id
		}
		body, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(stdin, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(accept func(rpcMessage) bool) rpcMessage {
		t.Helper()
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		for {
			select {
			case result := <-responses:
				if result.err != nil {
					t.Fatalf("LSP stream: %v; stderr: %s", result.err, stderr.String())
				}
				if accept(result.message) {
					return result.message
				}
			case <-timer.C:
				t.Fatalf("LSP response timeout; stderr: %s", stderr.String())
			}
		}
	}
	send("initialize", map[string]any{"processId": nil, "rootUri": nil, "capabilities": map[string]any{}}, 1)
	initialized := wait(func(m rpcMessage) bool { return string(m.ID) == "1" })
	var capabilities struct {
		Capabilities struct {
			DefinitionProvider bool `json:"definitionProvider"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(initialized.Result, &capabilities); err != nil {
		t.Fatalf("LSP initialization failed: %s, %v", initialized.Error, err)
	}
	if !capabilities.Capabilities.DefinitionProvider {
		t.Fatal("compiled LSP did not advertise definitions")
	}
	send("initialized", map[string]any{}, 0)
	uri, err := pathutil.ToFileURI(filepath.Join(home, "workload.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := "apiVersion: openchoreo.dev/v1alpha1\nkind: Workload\nmetadata:\n  name: example\nspec:\n  "
	send("textDocument/didOpen", map[string]any{textDocumentKey: map[string]any{
		uriKey: uri, "languageId": "yaml", "version": 1, "text": text,
	}}, 0)
	send("textDocument/completion", map[string]any{
		textDocumentKey: map[string]string{uriKey: uri}, "position": map[string]int{"line": 5, "character": 2},
	}, 2)
	completed := wait(func(m rpcMessage) bool { return string(m.ID) == "2" })
	var items []struct{ Label string }
	if err := json.Unmarshal(completed.Result, &items); err != nil {
		var list struct{ Items []struct{ Label string } }
		if err := json.Unmarshal(completed.Result, &list); err != nil {
			t.Fatal(err)
		}
		items = list.Items
	}
	found := false
	for _, item := range items {
		found = found || item.Label == "container"
	}
	if !found {
		t.Fatalf("schema completion missing container: %s", completed.Result)
	}
	text += "container:\n    image: nginx\n    " + invalidField + ": broken\n"
	send("textDocument/didChange", map[string]any{
		textDocumentKey:  map[string]any{uriKey: uri, "version": 2},
		"contentChanges": []map[string]string{{"text": text}},
	}, 0)
	wait(func(m rpcMessage) bool {
		return m.Method == "textDocument/publishDiagnostics" && bytes.Contains(m.Params, []byte(invalidField))
	})
	send("textDocument/hover", map[string]any{
		textDocumentKey: map[string]string{uriKey: uri}, "position": map[string]int{"line": 6, "character": 6},
	}, 3)
	hover := wait(func(m rpcMessage) bool { return string(m.ID) == "3" })
	if len(hover.Result) == 0 || string(hover.Result) == "null" {
		t.Fatal("compiled LSP did not return hover content")
	}
	assertCLIValidation(t, binary, home, text, env)
}

func assertCLIValidation(t *testing.T, binary, home, text string, env map[string]string) {
	t.Helper()
	writeTestFile(t, filepath.Join(home, "workload.yaml"), text)
	c := &coordinator{log: io.Discard}
	args := []string{"lint", "vali", "-b", home, "--template-version"}
	result, err := c.runCommand(home, env, binary, append(slices.Clone(args), "latest")...)
	if err == nil || !bytes.Contains(result, []byte(invalidField)) {
		t.Fatalf("compiled linter failed to validate YAML: %s, %v", result, err)
	}
	result, err = c.runCommand(home, env, binary, append(slices.Clone(args), "v0.0.1")...)
	if err == nil || !strings.Contains(strings.ToLower(string(result)), "available") {
		t.Fatalf("compiled linter failed to reject historical version: %s, %v", result, err)
	}
}

func TestReleaseBinary(t *testing.T) {
	binary := os.Getenv("OCC_RELEASE_BINARY")
	if binary == "" {
		t.Skip("set OCC_RELEASE_BINARY to smoke test a native release executable")
	}
	smokeReleaseBinary(t, binary)
}
