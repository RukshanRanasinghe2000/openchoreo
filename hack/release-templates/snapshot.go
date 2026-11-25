// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/openchoreo/openchoreo/internal/pathutil"
)

const (
	metadataFile = "release-snapshot.json"
	manifestFile = "template-version.json"
	generatorDir = "tools/lang-server-utils"
)

type snapshot struct {
	ReleaseVersion  string            `json:"releaseVersion"`
	TemplateVersion string            `json:"templateVersion"`
	SchemaFiles     []string          `json:"schemaFiles"`
	CELFiles        []string          `json:"celFiles"`
	Files           map[string]string `json:"files"`
}

type manifest struct {
	Latest   string   `json:"latest"`
	Versions []string `json:"versions"`
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func inventory(directory string) (map[string]string, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := make(map[string]string)
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in template snapshot: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if relative == metadataFile {
			return nil
		}
		data, err := root.ReadFile(relative)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		files[filepath.ToSlash(relative)] = hex.EncodeToString(digest[:])
		return nil
	})
	return files, err
}

func expectedPaths(meta snapshot) (map[string]bool, error) {
	paths := map[string]bool{manifestFile: true}
	groups := []struct {
		prefix string
		names  []string
	}{
		{"template-" + meta.TemplateVersion + "/", meta.SchemaFiles},
		{"cel-envs/" + strings.TrimPrefix(meta.TemplateVersion, "v") + "/", meta.CELFiles},
	}
	for _, group := range groups {
		if len(group.names) == 0 {
			return nil, fmt.Errorf("both schema and CEL output lists are required")
		}
		for _, name := range group.names {
			if name == "" || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".json") {
				return nil, fmt.Errorf("invalid generator filename %q", name)
			}
			path := group.prefix + name
			if paths[path] {
				return nil, fmt.Errorf("duplicate generator filename %q", name)
			}
			paths[path] = true
		}
	}
	return paths, nil
}

func (c *coordinator) validate(directory string) (snapshot, error) {
	var meta snapshot
	version, template, err := readVersion(c.root)
	if err != nil {
		return meta, err
	}
	if err := readJSON(filepath.Join(directory, metadataFile), &meta); err != nil {
		return meta, err
	}
	if meta.ReleaseVersion != version || meta.TemplateVersion != template {
		return meta, fmt.Errorf("template snapshot does not match VERSION")
	}
	paths, err := expectedPaths(meta)
	if err != nil {
		return meta, err
	}
	files, err := inventory(directory)
	if err != nil {
		return meta, err
	}
	if len(paths) != len(files) || !maps.Equal(files, meta.Files) {
		return meta, fmt.Errorf("snapshot has missing, modified, unexpected or historical files")
	}
	for path := range files {
		if !paths[path] {
			return meta, fmt.Errorf("unexpected or historical file %s", path)
		}
		if err := validateContent(directory, path, template); err != nil {
			return meta, err
		}
	}
	var actual manifest
	var manifestKeys map[string]json.RawMessage
	if err := readJSON(filepath.Join(directory, manifestFile), &manifestKeys); err != nil {
		return meta, err
	}
	if len(manifestKeys) != 2 || manifestKeys["latest"] == nil || manifestKeys["versions"] == nil {
		return meta, fmt.Errorf("manifest must contain only latest and versions")
	}
	if err := readJSON(filepath.Join(directory, manifestFile), &actual); err != nil {
		return meta, err
	}
	if actual.Latest != template || !slices.Equal(actual.Versions, []string{template}) {
		return meta, fmt.Errorf("manifest must contain exactly the release template version")
	}
	return meta, nil
}

func validateContent(directory, path, version string) error {
	var data struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Kind       string                     `json:"kind"`
		Variables  []json.RawMessage          `json:"variables"`
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if err := readJSON(filepath.Join(directory, filepath.FromSlash(path)), &data); err != nil {
		return err
	}
	if strings.HasPrefix(path, "template-"+version+"/") && (data.Type != "object" || len(data.Properties) == 0) {
		return fmt.Errorf("incomplete JSON schema %s", path)
	}
	if strings.HasPrefix(path, "cel-envs/") {
		if filepath.Base(path) != strings.ToLower(data.Kind)+"_celenv.json" ||
			len(data.Variables) == 0 || len(data.Extensions) == 0 {
			return fmt.Errorf("incomplete CEL environment %s", path)
		}
	}
	return nil
}

func (c *coordinator) generate(output string) error {
	version, template, err := readVersion(c.root)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	if pathutil.Within(output, c.root) ||
		(pathutil.Within(c.root, output) && !pathutil.Within(filepath.Join(c.root, "bin"), output)) {
		return fmt.Errorf("snapshot output must be outside source directories; use bin/release-templates")
	}
	if info, err := os.Lstat(output); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to replace non-directory snapshot %s", output)
		}
		if _, err := os.Stat(filepath.Join(output, metadataFile)); err != nil {
			return fmt.Errorf("refusing to overwrite non-snapshot directory %s", output)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(output), ".occ-template-generation-")
	if err != nil {
		return err
	}
	backup := filepath.Join(stage, "previous")
	published := false
	defer func() {
		if !published {
			if _, err := os.Stat(backup); err == nil {
				fmt.Fprintln(c.log, "Previous snapshot retained for recovery:", backup)
				return
			}
		}
		_ = os.RemoveAll(stage)
	}()
	assets := filepath.Join(stage, "templates")
	if err := os.Mkdir(assets, 0o755); err != nil {
		return err
	}
	config := filepath.Join(stage, "generator.json")
	input := map[string]any{
		"templatesDir": assets,
		"sources":      []map[string]string{{"root": c.root, "version": template}},
	}
	if err := writeJSON(config, input); err != nil {
		return err
	}
	meta := snapshot{ReleaseVersion: version, TemplateVersion: template}
	for _, generator := range []struct {
		name   string
		list   *[]string
		strict bool
	}{
		{"json-schema-generator", &meta.SchemaFiles, true},
		{"cel-env-generator", &meta.CELFiles, false},
	} {
		args := []string{"run", "./" + generatorDir + "/" + generator.name}
		list, err := c.run(c.root, nil, "go", append(slices.Clone(args), "--list-files")...)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(list, generator.list); err != nil {
			return fmt.Errorf("%s output list: %w", generator.name, err)
		}
		args = append(args, "--config", config, "--template-version", template, "--no-manifest")
		if generator.strict {
			args = append(args, "--strict")
		}
		result, err := c.run(c.root, nil, "go", args...)
		if err != nil {
			return err
		}
		fmt.Fprint(c.log, string(result))
	}
	if err := writeJSON(filepath.Join(assets, manifestFile), manifest{template, []string{template}}); err != nil {
		return err
	}
	meta.Files, err = inventory(assets)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(assets, metadataFile), meta); err != nil {
		return err
	}
	if _, err := c.validate(assets); err != nil {
		return err
	}
	if err := publishSnapshot(assets, output, backup); err != nil {
		return err
	}
	published = true
	fmt.Fprintln(c.log, "Validated latest-only snapshot:", output)
	return nil
}

func publishSnapshot(assets, output, backup string) error {
	previous := false
	if _, err := os.Stat(output); err == nil {
		if err := os.Rename(output, backup); err != nil {
			return err
		}
		previous = true
	}
	if err := os.Rename(assets, output); err != nil {
		if previous {
			if restoreErr := os.Rename(backup, output); restoreErr != nil {
				return fmt.Errorf("publish: %w; restore previous snapshot: %w", err, restoreErr)
			}
		}
		return err
	}
	return nil
}
