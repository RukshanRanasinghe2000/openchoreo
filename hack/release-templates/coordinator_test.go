// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	testTemplate  = "v1.5.0"
	failureCEL    = "cel"
	failureSchema = "schema"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) (*coordinator, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "source with spaces")
	writeTestFile(t, filepath.Join(root, "VERSION"), "1.5.0\n")
	c, err := newCoordinator(root, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return c, filepath.Join(base, "snapshot with spaces")
}

func fakeGenerator(t *testing.T, fail string) commandRunner {
	t.Helper()
	return func(_ string, _ map[string]string, name string, args ...string) ([]byte, error) {
		if name != "go" || len(args) < 3 || args[0] != "run" {
			t.Fatalf("unexpected generator command: %s %v", name, args)
		}
		cel := strings.HasSuffix(args[1], "/cel-env-generator")
		file := "project.json"
		if cel {
			file = "project_celenv.json"
		}
		if slices.Contains(args, "--list-files") {
			return json.Marshal([]string{file})
		}
		if fail == failureSchema && !cel || fail == failureCEL && cel {
			return nil, fmt.Errorf("simulated %s generator failure", fail)
		}
		if !slices.Contains(args, "--no-manifest") || !cel && !slices.Contains(args, "--strict") {
			t.Fatal("release must coordinate the manifest and require complete schemas")
		}
		var config struct {
			TemplatesDir string `json:"templatesDir"`
		}
		if err := readJSON(args[slices.Index(args, "--config")+1], &config); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(config.TemplatesDir, manifestFile)); !os.IsNotExist(err) {
			t.Fatal("manifest published before both generators finished")
		}
		version := args[slices.Index(args, "--template-version")+1]
		path := filepath.Join(config.TemplatesDir, "template-"+version, file)
		content := `{"type":"object","properties":{"spec":{"type":"object"}}}`
		if cel {
			path = filepath.Join(config.TemplatesDir, "cel-envs", strings.TrimPrefix(version, "v"), file)
			content = `{"kind":"Project","variables":[{"name":"metadata"}],"extensions":{"base":["strings"]}}`
		}
		writeTestFile(t, path, content)
		return nil, nil
	}
}

func generatedFixture(t *testing.T) (*coordinator, string, snapshot) {
	t.Helper()
	c, path := fixture(t)
	c.run = fakeGenerator(t, "")
	if err := c.generate(path); err != nil {
		t.Fatal(err)
	}
	meta, err := c.validate(path)
	if err != nil {
		t.Fatal(err)
	}
	return c, path, meta
}

func TestReleaseVersions(t *testing.T) {
	c, _ := fixture(t)
	for _, value := range []string{"1.5.0", "1.5.0-rc.1", "1.5.0-m.2+build.3"} {
		writeTestFile(t, filepath.Join(c.root, "VERSION"), value+"\n")
		actual, template, err := readVersion(c.root)
		if err != nil || actual != value || template != testTemplate {
			t.Fatalf("version %q: %s %s %v", value, actual, template, err)
		}
	}
	for _, value := range []string{"", "v1.5.0", "1.5", "01.5.0", "1.5.0-01", "1.5.0-", "latest", "1.5.0\n1.6.0"} {
		writeTestFile(t, filepath.Join(c.root, "VERSION"), value)
		if _, _, err := readVersion(c.root); err == nil {
			t.Fatalf("accepted invalid version %q", value)
		}
	}
}

func TestGenerationDeterminismAndFailurePreservesPreviousSnapshot(t *testing.T) {
	c, path, _ := generatedFixture(t)
	first, err := os.ReadFile(filepath.Join(path, metadataFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.generate(path); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{failureSchema, failureCEL} {
		c.run = fakeGenerator(t, failure)
		if err := c.generate(path); err == nil {
			t.Fatalf("ignored %s generator failure", failure)
		}
		actual, err := os.ReadFile(filepath.Join(path, metadataFile))
		if err != nil || !bytes.Equal(first, actual) {
			t.Fatal("failed generation changed the previous snapshot")
		}
		entries, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".occ-template-generation-*"))
		if err != nil || len(entries) != 0 {
			t.Fatal("generation did not clean staging storage")
		}
	}
	newOutput := filepath.Join(t.TempDir(), "failed snapshot")
	if err := c.generate(newOutput); err == nil {
		t.Fatal("expected generator failure")
	}
	if _, err := os.Stat(newOutput); !os.IsNotExist(err) {
		t.Fatal("failed generation published a partial snapshot")
	}
}

func TestPrereleaseUsesNormalizedTemplateAndFullBinaryVersion(t *testing.T) {
	c, path := fixture(t)
	writeTestFile(t, filepath.Join(c.root, "VERSION"), "1.5.0-rc.1+build.2")
	c.run = fakeGenerator(t, "")
	if err := c.generate(path); err != nil {
		t.Fatal(err)
	}
	meta, err := c.validate(path)
	if err != nil || meta.ReleaseVersion != "1.5.0-rc.1+build.2" || meta.TemplateVersion != testTemplate {
		t.Fatalf("incorrect prerelease snapshot: %+v, %v", meta, err)
	}
	writeTestFile(t, filepath.Join(c.root, "VERSION"), "1.6.0")
	if _, err := c.validate(path); err == nil {
		t.Fatal("accepted snapshot for another release")
	}
}

func updateTestMetadata(t *testing.T, path string, meta snapshot) {
	t.Helper()
	var err error
	meta.Files, err = inventory(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(path, metadataFile), meta); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotRejectsInvalidOrHistoricalAssets(t *testing.T) {
	tests := map[string]func(*testing.T, string, snapshot){
		"modified": func(t *testing.T, path string, _ snapshot) {
			writeTestFile(t, filepath.Join(path, "template-v1.5.0", "project.json"), "{}")
		},
		"missing": func(t *testing.T, path string, _ snapshot) {
			if err := os.Remove(filepath.Join(path, "cel-envs", "1.5.0", "project_celenv.json")); err != nil {
				t.Fatal(err)
			}
		},
		"historical": func(t *testing.T, path string, meta snapshot) {
			writeTestFile(t, filepath.Join(path, "template-v1.3.0", "old.json"), "{}")
			updateTestMetadata(t, path, meta)
		},
		"schema": func(t *testing.T, path string, meta snapshot) {
			writeTestFile(t, filepath.Join(path, "template-v1.5.0", "project.json"), "{}")
			updateTestMetadata(t, path, meta)
		},
		failureCEL: func(t *testing.T, path string, meta snapshot) {
			writeTestFile(t, filepath.Join(path, "cel-envs", "1.5.0", "project_celenv.json"), `{"kind":"Other"}`)
			updateTestMetadata(t, path, meta)
		},
		"manifest": func(t *testing.T, path string, meta snapshot) {
			writeTestFile(t, filepath.Join(path, manifestFile), `{"latest":"v1.3.0","versions":["v1.3.0","v1.5.0"]}`)
			updateTestMetadata(t, path, meta)
		},
		"manifest extra fields": func(t *testing.T, path string, meta snapshot) {
			writeTestFile(t, filepath.Join(path, manifestFile), `{"latest":"v1.5.0","versions":["v1.5.0"],"old":"v1.3.0"}`)
			updateTestMetadata(t, path, meta)
		},
		"duplicate": func(t *testing.T, path string, meta snapshot) {
			meta.SchemaFiles = append(meta.SchemaFiles, meta.SchemaFiles[0])
			updateTestMetadata(t, path, meta)
		},
		"traversal": func(t *testing.T, path string, meta snapshot) {
			meta.SchemaFiles = []string{`..\escape.json`}
			updateTestMetadata(t, path, meta)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c, path, meta := generatedFixture(t)
			mutate(t, path, meta)
			if _, err := c.validate(path); err == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
}

func TestPreparePreservesSourceAndExcludesHistory(t *testing.T) {
	c, path, meta := generatedFixture(t)
	writeTestFile(t, filepath.Join(c.root, "tools", "templates", "template-v1.3.0", "old.json"), "{}")
	writeTestFile(t, filepath.Join(c.root, "tools", "templates", "cel-envs", "1.3.0", "old.json"), "{}")
	writeTestFile(t, filepath.Join(c.root, "tools", "templates", "embed.go"), "package templates\n")
	writeTestFile(t, filepath.Join(c.root, "uncommitted.go"), "package example\n")
	before, err := inventory(c.root)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "isolated source")
	if err := c.prepare(path, workspace); err != nil {
		t.Fatal(err)
	}
	for _, historical := range []string{"template-v1.3.0", "cel-envs/1.3.0"} {
		target := filepath.Join(workspace, "tools", "templates", filepath.FromSlash(historical))
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("historical assets survived in the build workspace")
		}
	}
	for _, file := range []string{filepath.Join("tools", "templates", "embed.go"), "uncommitted.go"} {
		if _, err := os.Stat(filepath.Join(workspace, file)); err != nil {
			t.Fatal("source file lost:", err)
		}
	}
	after, err := inventory(c.root)
	if err != nil || !maps.Equal(before, after) {
		t.Fatal("preparation modified the original source")
	}
	embedded, err := inventory(filepath.Join(workspace, "tools", "templates"))
	if err != nil {
		t.Fatal(err)
	}
	delete(embedded, "embed.go")
	if !maps.Equal(embedded, meta.Files) {
		t.Fatal("build workspace does not contain exactly the snapshot")
	}
	if err := c.prepare(path, workspace); err == nil {
		t.Fatal("overwrote an existing workspace")
	}
	if err := c.generate(filepath.Join(c.root, "tools", "templates")); err == nil {
		t.Fatal("generation accepted the source template directory")
	}
	if err := c.prepare(path, c.root); err == nil {
		t.Fatal("preparation accepted the original checkout")
	}
}

func TestSnapshotPublicationRollsBack(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "snapshot")
	writeTestFile(t, filepath.Join(output, "sentinel"), "previous")
	err := publishSnapshot(filepath.Join(dir, "missing"), output, filepath.Join(dir, "backup"))
	if err == nil {
		t.Fatal("expected publication failure")
	}
	actual, err := os.ReadFile(filepath.Join(output, "sentinel"))
	if err != nil || string(actual) != "previous" {
		t.Fatal("publication failure lost the previous snapshot")
	}
}

func TestBuildUsesMakeAndCleansIsolatedWorkspace(t *testing.T) {
	c, path, meta := generatedFixture(t)
	writeTestFile(t, filepath.Join(c.root, "tools", "templates", "embed.go"), "package templates\n")
	var buildWorkspace string
	c.run = func(dir string, env map[string]string, name string, args ...string) ([]byte, error) {
		switch name {
		case "git":
			if args[0] == "show" {
				return []byte("1700000000"), nil
			}
			return []byte("abcd1234"), nil
		case "make":
			buildWorkspace = dir
			if !slices.Contains(args, "go.build.occ") || !slices.Contains(args, "GO_BUILD_FLAGS=-trimpath -buildvcs=false") {
				t.Fatal("build bypassed existing Make infrastructure")
			}
			var output, target string
			for _, arg := range args {
				if strings.HasPrefix(arg, "GO_BUILD_OUTPUT_DIR=") {
					output = strings.TrimPrefix(arg, "GO_BUILD_OUTPUT_DIR=")
				}
				if strings.HasPrefix(arg, "GO_CURRENT_PLATFORM=") {
					target = strings.TrimPrefix(arg, "GO_CURRENT_PLATFORM=")
				}
			}
			writeTestFile(t, filepath.Join(output, filepath.FromSlash(target), "occ"), "fake native executable")
			return nil, nil
		default:
			if dir == c.root || env["HOME"] == "" || env["USERPROFILE"] == "" {
				t.Fatal("verification did not isolate cwd/home")
			}
			if slices.Contains(args, "--templates") {
				return json.Marshal(binaryInventory{
					meta.ReleaseVersion, meta.TemplateVersion, []string{meta.TemplateVersion}, meta.Files,
				})
			}
			if slices.Contains(args, "select") {
				return []byte("unavailable; available: v1.5.0"), errors.New("expected rejection")
			}
			return []byte(testTemplate), nil
		}
	}
	if err := c.build(path, "", filepath.Join(t.TempDir(), "output")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(buildWorkspace); !os.IsNotExist(err) {
		t.Fatal("build workspace was not cleaned")
	}
	successfulRunner := c.run
	c.run = func(dir string, env map[string]string, name string, args ...string) ([]byte, error) {
		if name == "make" {
			buildWorkspace = dir
			return nil, errors.New("compiler failed")
		}
		return successfulRunner(dir, env, name, args...)
	}
	if err := c.build(path, "", filepath.Join(t.TempDir(), "failed")); err == nil {
		t.Fatal("ignored compiler failure")
	}
	if _, err := os.Stat(buildWorkspace); !os.IsNotExist(err) {
		t.Fatal("failed build did not clean its workspace")
	}
	for _, invalid := range []string{"linux/386", "freebsd/amd64", "windows", "../../escape"} {
		if _, err := releasePlatforms(invalid); err == nil {
			t.Fatalf("accepted unsupported platform %q", invalid)
		}
	}
}

func TestCommandValidation(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"prepare"}, {"verify"}, {"generate", "unexpected"}} {
		if err := execute(args, io.Discard); err == nil {
			t.Fatalf("accepted invalid command: %v", args)
		}
	}
}

func TestVerificationRejectsMismatchedExecutable(t *testing.T) {
	for _, failure := range []string{"wrong version", "history", "digest", "malformed"} {
		t.Run(failure, func(t *testing.T) {
			c, path, meta := generatedFixture(t)
			actual := binaryInventory{
				meta.ReleaseVersion, meta.TemplateVersion, []string{meta.TemplateVersion}, maps.Clone(meta.Files),
			}
			switch failure {
			case "wrong version":
				actual.Version = "1.3.0"
			case "history":
				actual.Files["template-v1.3.0/old.json"] = "unexpected"
			case "digest":
				actual.Files[manifestFile] = "changed"
			}
			c.run = func(_ string, _ map[string]string, _ string, _ ...string) ([]byte, error) {
				if failure == "malformed" {
					return []byte("not JSON"), nil
				}
				return json.Marshal(actual)
			}
			if err := c.verify(path, filepath.Join(t.TempDir(), "occ.exe")); err == nil {
				t.Fatal("accepted mismatched compiled assets")
			}
		})
	}
}
