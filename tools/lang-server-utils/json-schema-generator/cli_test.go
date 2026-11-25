// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestGeneratorCLI(t *testing.T) {
	root := filepath.Join(t.TempDir(), "working directory with spaces")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "json-schema-generator")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build generator: %v\n%s", err, output)
	}
	write := func(name, content string) {
		t.Helper()
		name = filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fixture := "package v1alpha1\ntype Project struct { Spec ProjectSpec `json:\"spec\"` }\n" +
		"type ProjectSpec struct {\n// +kubebuilder:validation:Enum=small;large\nSize string `json:\"size\"`\n}\n"
	write("source one/api/v1alpha1/project_types.go", fixture)
	write("source two/api/v1alpha1/project_types.go", fixture)
	write("source two/CHANGELOG.md", "# Changelog\n\n## v1.3.0-rc.1\n")
	// Sources/output are relative to cwd, even though the config is nested.
	config := `{"templatesDir":"generated schemas","sources":[
		{"root":"source one","version":"v1.2.0"},{"root":"source two"}]}`
	write("configs with spaces/generator.json", config)
	run := func(wantOK bool, args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if (err == nil) != wantOK {
			t.Fatalf("run %v: %v\n%s", args, err, output)
		}
		return string(output)
	}
	run(true, "--config", filepath.Join("configs with spaces", "generator.json"))
	snapshot := func() map[string][]byte {
		t.Helper()
		files := make(map[string][]byte)
		visit := func(name string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			if !json.Valid(data) || !bytes.HasSuffix(data, []byte("\n")) {
				t.Errorf("invalid JSON or missing final newline: %s", name)
			}
			files[name] = data
			return nil
		}
		err := filepath.WalkDir(filepath.Join(root, "generated schemas"), visit)
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	first := snapshot()
	for _, version := range []string{"v1.2.0", "v1.3.0"} {
		name := filepath.Join(root, "generated schemas", "template-"+version, "project_minimal_defi.json")
		var schema Field
		if err := json.Unmarshal(first[name], &schema); err != nil {
			t.Fatalf("missing or invalid schema for %s: %v", version, err)
		}
		spec := schema.Properties["spec"]
		if spec == nil || spec.Properties["size"] == nil {
			t.Fatalf("missing spec.size in generated schema for %s", version)
		}
		if !reflect.DeepEqual(spec.Properties["size"].Enum, []string{"small", "large"}) {
			t.Fatalf("enum not preserved in generated schema for %s", version)
		}
	}
	manifest := filepath.Join(root, "generated schemas", versionFileName)
	var got struct {
		Latest   string   `json:"latest"`
		Versions []string `json:"versions"`
	}
	if err := json.Unmarshal(first[manifest], &got); err != nil {
		t.Fatal(err)
	}
	if got.Latest != "v1.3.0" || !reflect.DeepEqual(got.Versions, []string{"v1.2.0", "v1.3.0"}) {
		t.Fatalf("invalid manifest: %+v", got)
	}
	run(true, "--config", filepath.Join("configs with spaces", "generator.json"))
	if !reflect.DeepEqual(first, snapshot()) {
		t.Fatal("generation is not deterministic")
	}
	// A compiled binary retains support for a sidecar config without --config.
	write(configFileName, config)
	run(true)
	if !reflect.DeepEqual(first, snapshot()) {
		t.Fatal("sidecar config changed generation output")
	}
	write("bad.json", `{"templatesDir":"invalid output","sources":[{"root":"source one","version":"wrong"}]}`)
	if output := run(false, "--config", "bad.json"); !strings.Contains(output, "invalid version") {
		t.Fatalf("missing invalid-version error: %s", output)
	}
	write("missing-source.json", `{"templatesDir":"invalid output","sources":[{"root":"missing","version":"v1.2.0"}]}`)
	if output := run(false, "--config", "missing-source.json"); !strings.Contains(output, "cannot access api package") {
		t.Fatalf("missing source error: %s", output)
	}
	run(false, "--config", "missing-config.json")
	if _, err := os.Stat(filepath.Join(root, "invalid output")); !os.IsNotExist(err) {
		t.Fatal("invalid config/source wrote output")
	}
	write("release.json", `{"templatesDir":"release output","sources":[{"root":"source one","version":"v1.2.0"}]}`)
	run(true, "--config", "release.json", "--template-version", "v1.5.0", "--no-manifest")
	releaseSchema := filepath.Join(root, "release output", "template-v1.5.0", "project_minimal_defi.json")
	if _, err := os.Stat(releaseSchema); err != nil {
		t.Fatal("explicit release version did not override the configured version", err)
	}
	if _, err := os.Stat(filepath.Join(root, "release output", versionFileName)); !os.IsNotExist(err) {
		t.Fatal("--no-manifest wrote the shared manifest")
	}
	output := run(false, "--config", "release.json", "--template-version", "v1.5.0", "--strict", "--no-manifest")
	if !strings.Contains(output, "not found") {
		t.Fatalf("strict generation must reject an incomplete API source: %s", output)
	}
}
