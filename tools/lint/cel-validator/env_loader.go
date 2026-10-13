// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package celvalidator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

var celEnvDir = ""

func SetCelEnvDir(dir string) { celEnvDir = dir }
func GetCelEnvDir() string    { return celEnvDir }

type envDesc struct {
	Kind       string `json:"kind"`
	HasApplied bool   `json:"hasApplied"`
}

func loadEnvDesc(kind string) (*envDesc, bool) {
	if celEnvDir == "" {
		return nil, false
	}
	path := filepath.Join(celEnvDir, strings.ToLower(kind)+"_celenv.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var d envDesc
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, false
	}
	return &d, true
}
