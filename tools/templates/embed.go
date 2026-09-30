// Copyright 2025 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

// Package templates carries the generated OpenChoreo JSON schema sets that the
// linter validates against.
//
// The files live in their own directory rather than inside the Go package that
// reads them so that `make lint-schemas` can regenerate them from api/v1alpha1
// and so they stay greppable and diffable on their own. That placement costs
// the obvious thing: a //go:embed directive can only reach files inside its own
// package directory, so the embed has to live here rather than in
// tools/lint/ruleengine/template, which consumes FS.
package templates

import "embed"

// FS holds every template version shipped with occ, plus the manifest that
// records which versions exist and which is the latest.
//
// The manifest is template-version.json and each version is a template-v*/
// subdirectory of JSON schema files, both produced by the schema generator
// (cmd/schema-gen) from the Go API types in api/v1alpha1.
//
//go:embed template-version.json template-v*/*.json
var FS embed.FS
