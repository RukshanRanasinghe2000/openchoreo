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

const configFileName = "cel-env-generator.config.json"

func defaultConfigPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), configFileName)
	}
	return configFileName
}

const defaultTemplatesDir = "internal/ruleengine/template"

type sourceConfig struct {
	Root    string `json:"root"`
	API     string `json:"api,omitempty"`
	Version string `json:"version,omitempty"`
}

type Config struct {
	TemplatesDir string         `json:"templatesDir,omitempty"`
	Sources      []sourceConfig `json:"sources,omitempty"`
}

func (c *Config) TemplatesDirOrDefault() string {
	if c.TemplatesDir == "" {
		return defaultTemplatesDir
	}
	return c.TemplatesDir
}

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
