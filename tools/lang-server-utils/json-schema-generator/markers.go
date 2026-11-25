// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// markerSet holds the kubebuilder/+ markers parsed from doc comments: fields
// maps "Type" -> "Field" -> markers, types maps "Type" -> markers.
type markerSet struct {
	fields map[string]map[string][]marker
	types  map[string][]marker
}

// collectMarkers walks top-level type declarations and records every
// kubebuilder/+ marker attached to each named type and struct field.
func collectMarkers(files []*ast.File) markerSet {
	ms := markerSet{
		fields: map[string]map[string][]marker{},
		types:  map[string][]marker{},
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				typeName := ts.Name.Name
				if mm := parseTypeMarkers(gen, ts); len(mm) > 0 {
					ms.types[typeName] = mm
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				if ms.fields[typeName] == nil {
					ms.fields[typeName] = map[string][]marker{}
				}
				for _, field := range st.Fields.List {
					names := field.Names
					if len(names) == 0 {
						names = []*ast.Ident{embeddedName(field.Type)}
					}
					mm := parseMarkers(field.Doc)
					mm = append(mm, parseMarkers(field.Comment)...)
					for _, n := range names {
						if n == nil {
							continue
						}
						ms.fields[typeName][n.Name] = append(ms.fields[typeName][n.Name], mm...)
					}
				}
			}
		}
	}
	return ms
}

// parseTypeMarkers extracts the markers attached to a type declaration, from
// both the GenDecl doc and the TypeSpec.
func parseTypeMarkers(gen *ast.GenDecl, ts *ast.TypeSpec) []marker {
	var mm []marker
	mm = append(mm, parseMarkers(gen.Doc)...)
	mm = append(mm, parseMarkers(ts.Doc)...)
	mm = append(mm, parseMarkers(ts.Comment)...)
	return mm
}

// embeddedName returns the name used for an embedded (anonymous) field.
func embeddedName(expr ast.Expr) *ast.Ident {
	switch t := expr.(type) {
	case *ast.Ident:
		return t
	case *ast.SelectorExpr:
		return t.Sel
	}
	return nil
}

// parseMarkers extracts "+..." markers from a comment group.
func parseMarkers(cg *ast.CommentGroup) []marker {
	if cg == nil {
		return nil
	}
	var ms []marker
	for _, c := range cg.List {
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Text), "//"))
		if !strings.HasPrefix(text, "+") {
			continue
		}
		name := text[1:]
		value := ""
		if i := strings.Index(name, "="); i >= 0 {
			value = name[i+1:]
			name = name[:i]
		}
		// The `+kubebuilder:default:=<value>` form leaves a trailing colon on
		// the name; normalize it so both forms map to the same marker.
		if name == "kubebuilder:default:" {
			name = "kubebuilder:default"
		}
		if name == "kubebuilder:default" {
			// Keep the default value raw (quotes included): it is parsed as
			// YAML so the emitted JSON default has the correct type.
			ms = append(ms, marker{name: name, value: value})
			continue
		}
		ms = append(ms, marker{name: name, value: unquote(value)})
	}
	return ms
}

// unquote strips matching surrounding backticks or quotes from a marker value.
func unquote(s string) string {
	if len(s) >= 2 {
		switch s[0] {
		case '`', '"', '\'':
			if s[len(s)-1] == s[0] {
				return s[1 : len(s)-1]
			}
		}
	}
	return s
}

// applyMarkers transfers marker constraints onto the schema field.
func applyMarkers(f *Field, ms []marker) {
	for _, m := range ms {
		switch m.name {
		case "optional", "kubebuilder:validation:Optional":
			f.Required = false
		case "required", "kubebuilder:validation:Required":
			f.Required = true
		case "kubebuilder:validation:MinLength":
			if v, err := strconv.Atoi(m.value); err == nil {
				f.MinLength = v
			}
		case "kubebuilder:validation:MaxLength":
			if v, err := strconv.Atoi(m.value); err == nil {
				f.MaxLength = v
			}
		case "kubebuilder:validation:MinItems":
			if v, err := strconv.Atoi(m.value); err == nil {
				f.MinItems = v
			}
		case "kubebuilder:validation:MaxItems":
			if v, err := strconv.Atoi(m.value); err == nil {
				f.MaxItems = v
			}
		case "kubebuilder:validation:Pattern":
			f.Pattern = m.value
		case "kubebuilder:validation:Format":
			f.Format = m.value
		case "kubebuilder:validation:Enum":
			f.Enum = splitEnum(m.value)
		case "kubebuilder:default":
			if dv := typedDefault(m.value); len(dv) > 0 {
				f.Default = dv
			}
		case "kubebuilder:validation:Minimum":
			if v, err := strconv.ParseInt(m.value, 10, 64); err == nil {
				f.Minimum = &v
			}
		case "kubebuilder:validation:Maximum":
			if v, err := strconv.ParseInt(m.value, 10, 64); err == nil {
				f.Maximum = &v
			}
		case "kubebuilder:pruning:PreserveUnknownFields":
			f.Preserve = true
		case "kubebuilder:validation:Schemaless":
			f.Type = "object"
			f.Preserve = true
		case "kubebuilder:validation:Type":
			f.Type = m.value
		}
	}
}

// splitEnum splits a ";"-separated enum marker value.
func splitEnum(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ";")
}

// typedDefault converts a YAML default marker value into a JSON default.
func typedDefault(raw string) json.RawMessage {
	if raw == "" {
		return nil
	}
	var v interface{}
	if err := yaml.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}
