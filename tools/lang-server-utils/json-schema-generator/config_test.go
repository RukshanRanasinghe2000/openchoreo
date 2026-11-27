// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigValid(t *testing.T) {
	// A full config parses, including per-source overrides.
	path := writeConfig(t, `{
		"templatesDir": "internal/ruleengine/template",
		"sources": [
			{ "root": "../openchoreo" },
			{
				"root": "../openchoreo-v1.3.0/openchoreo",
				"api": "../openchoreo-v1.3.0/openchoreo/api/v1alpha1",
				"version": "v1.3.0"
			}
		]
	}`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.TemplatesDirOrDefault() != "internal/ruleengine/template" {
		t.Errorf("templatesDir = %q", cfg.TemplatesDirOrDefault())
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(cfg.Sources))
	}
	if cfg.Sources[1].Version != testTemplateV130 {
		t.Errorf("source[1].version = %q, want v1.3.0", cfg.Sources[1].Version)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	path := writeConfig(t, `{ "sources": [{ "root": "../openchoreo" }] }`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.TemplatesDirOrDefault() != defaultTemplatesDir {
		t.Errorf("templatesDir default = %q, want %q", cfg.TemplatesDirOrDefault(), defaultTemplatesDir)
	}
}

func TestLoadConfigMissingRoot(t *testing.T) {
	path := writeConfig(t, `{ "sources": [{ "version": "v1.3.0" }] }`)
	if _, err := loadConfig(path); err == nil {
		t.Fatal("expected error for a source without root")
	}
}

func TestLoadConfigUnknownField(t *testing.T) {
	path := writeConfig(t, `{ "sources": [{ "root": "../openchoreo" }], "outputDir": "/tmp" }`)
	if _, err := loadConfig(path); err == nil {
		t.Fatal("expected error for an unknown config field")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	if _, err := loadConfig(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for a missing config file")
	}
}

func TestResolveJobsFromConfig(t *testing.T) {
	config := writeConfig(t, `{
		"sources": [
			{ "root": "../openchoreo" },
			{ "root": "../openchoreo-v1.3.0/openchoreo", "version": "v1.3.0" }
		]
	}`)
	jobs, templates, err := resolveJobs(config)
	if err != nil {
		t.Fatalf("resolveJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(jobs))
	}
	if jobs[0].root != "../openchoreo" || jobs[0].version != "" {
		t.Errorf("job[0] = %+v (want version resolved from CHANGELOG later)", jobs[0])
	}
	if jobs[1].root != "../openchoreo-v1.3.0/openchoreo" || jobs[1].version != testTemplateV130 {
		t.Errorf("job[1] = %+v", jobs[1])
	}
	if jobs[1].apiDir != filepath.Join("..", "openchoreo-v1.3.0", "openchoreo", "api", "v1alpha1") {
		t.Errorf("job[1].apiDir = %q", jobs[1].apiDir)
	}
	if templates != defaultTemplatesDir {
		t.Errorf("templates = %q, want %q", templates, defaultTemplatesDir)
	}
}

func TestResolveJobVersion(t *testing.T) {
	// An unpinned job takes its version from CHANGELOG.md; a pinned one keeps it.
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("## v1.2.0-rc.1\n\nChanges\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := &job{root: repo, apiDir: filepath.Join(repo, "api", "v1alpha1")}
	if err := resolveJobVersion(j); err != nil {
		t.Fatalf("resolveJobVersion: %v", err)
	}
	if j.version != "v1.2.0" || !j.fromChangelog {
		t.Errorf("job = %+v, want version v1.2.0 from changelog", *j)
	}

	j2 := &job{root: repo, apiDir: filepath.Join(repo, "api", "v1alpha1"), version: testTemplateV130}
	if err := resolveJobVersion(j2); err != nil {
		t.Fatalf("resolveJobVersion (explicit): %v", err)
	}
	if j2.version != testTemplateV130 || j2.fromChangelog {
		t.Errorf("job = %+v, want pinned version v1.3.0", *j2)
	}
}

func TestResolveJobsInvalidExplicitVersion(t *testing.T) {
	config := writeConfig(t, `{ "sources": [{ "root": "../openchoreo", "version": "latest" }] }`)
	if _, _, err := resolveJobs(config); err == nil {
		t.Fatal("expected error for an invalid explicit version")
	}
}

func TestResolveJobsNoSourcesFallsBack(t *testing.T) {
	config := writeConfig(t, `{}`)
	jobs, _, err := resolveJobs(config)
	if err != nil {
		t.Fatalf("resolveJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].root != defaultRoot {
		t.Fatalf("jobs = %+v, want single %s job", jobs, defaultRoot)
	}
}
