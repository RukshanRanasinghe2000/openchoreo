// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// configFileName is the config file that travels with the binary. It lives in
// the same directory as the executable so the generator works wherever it is
// placed.
const configFileName = "json-schema-generator.config.json"

func defaultConfigPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), configFileName)
	}
	return configFileName
}

// defaultTemplatesDir is used when the config file omits templatesDir.
const defaultTemplatesDir = "tools/templates"

// sourceConfig describes one OpenChoreo checkout to generate schemas from.
type sourceConfig struct {
	// Root is the OpenChoreo repository root, containing CHANGELOG.md
	// unless Version is set.
	Root string `json:"root"`

	// API overrides <root>/api/v1alpha1 when non-empty.
	API string `json:"api,omitempty"`

	// Version pins the generated folder name (e.g. "v1.3.0"), bypassing
	// CHANGELOG.md.
	Version string `json:"version,omitempty"`
}

// Config is the on-disk shape of the generator configuration file.
type Config struct {
	// TemplatesDir holds the template-v* folders and template-version.json.
	TemplatesDir string         `json:"templatesDir,omitempty"`
	Sources      []sourceConfig `json:"sources,omitempty"`
}

// TemplatesDirOrDefault returns TemplatesDir, or the default when unset.
func (c *Config) TemplatesDirOrDefault() string {
	if c.TemplatesDir == "" {
		return defaultTemplatesDir
	}
	return c.TemplatesDir
}

// loadConfig reads and validates the configuration file. Unknown fields are
// rejected so typos do not silently change behavior.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, s := range cfg.Sources {
		if s.Root == "" {
			return nil, fmt.Errorf("%s: source is missing required field \"root\"", path)
		}
	}
	return &cfg, nil
}
