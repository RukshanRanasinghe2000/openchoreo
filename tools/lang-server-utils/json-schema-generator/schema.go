// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"go/constant"
	"go/types"
	"reflect"
	"sort"
	"strings"
)

// generator converts the type-checked v1alpha1 package into minimal JSON
// schemas for each resource kind.
type generator struct {
	pkg       *types.Package
	markers   markerSet
	stack     []*types.Named // guard against cyclic struct references
	enumCache map[*types.TypeName][]string
}

func newGenerator(pkg *types.Package, markers markerSet) *generator {
	return &generator{pkg: pkg, markers: markers}
}

// generateKind produces the full schema for a resource kind.
func (g *generator) generateKind(ks kindSpec) *Field {
	g.stack = nil
	obj := g.pkg.Scope().Lookup(ks.GoTypeName)
	if obj == nil {
		return nil
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return nil
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil
	}

	root := &Field{Type: schemaTypeObject, omitRequired: true}
	root.Properties = map[string]*Field{
		"apiVersion": {Type: schemaTypeString, Required: true, Default: json.RawMessage(`"openchoreo.dev/v1alpha1"`)},
		"kind":       {Type: schemaTypeString, Required: true, Default: kindDefault(ks.GoTypeName)},
	}
	root.Properties["metadata"] = ks.metadataBuilder()

	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		switch f.Name() {
		case "Spec":
			tag := st.Tag(i)
			_, _, omitempty, _ := parseJSONTag(tag)
			ms := g.markers.fields[ks.GoTypeName][f.Name()]
			root.Properties["spec"] = &Field{
				Type: schemaTypeObject, Required: isRequired(ms, omitempty), Properties: g.structProps(f.Type()),
			}
		case "Status":
			root.Properties["status"] = &Field{Type: schemaTypeObject, Required: false, Properties: g.structProps(f.Type())}
		}
	}
	return root
}

func kindDefault(goTypeName string) json.RawMessage {
	data, err := json.Marshal(goTypeName)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}

// structProps converts a struct type into a properties map.
func (g *generator) structProps(t types.Type) map[string]*Field {
	t = deref(t)
	named, _ := t.(*types.Named)
	if named != nil {
		for _, n := range g.stack {
			if n == named {
				return nil
			}
		}
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	typeName := g.typeName(t)
	if named != nil {
		g.stack = append(g.stack, named)
		defer func() { g.stack = g.stack[:len(g.stack)-1] }()
	}

	props := map[string]*Field{}
	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		tag := st.Tag(i)
		jsonName, inline, omitempty, skip := parseJSONTag(tag)
		if skip {
			continue
		}
		if inline {
			g.inlineFields(props, field.Type())
			continue
		}
		if isK8sNamed(field.Type(), "apis/meta/v1", "ObjectMeta") ||
			isK8sNamed(field.Type(), "apis/meta/v1", "TypeMeta") ||
			isK8sNamed(field.Type(), "apis/meta/v1", "ListMeta") {
			continue
		}
		name := jsonName
		if name == "" {
			name = lowerFirst(field.Name())
		}
		props[name] = g.fieldSchema(field, typeName, omitempty)
	}
	return props
}

func (g *generator) inlineFields(props map[string]*Field, t types.Type) {
	subProps := g.structProps(t)
	if subProps == nil {
		return
	}
	mergeProps(props, subProps)
}

func mergeProps(dst, src map[string]*Field) {
	for k, v := range src {
		dst[k] = v
	}
}

// fieldSchema converts a single struct field into a schema node.
func (g *generator) fieldSchema(field *types.Var, typeName string, omitempty bool) *Field {
	ms := g.markers.fields[typeName][field.Name()]

	f := &Field{}
	g.fillType(f, field.Type())
	applyMarkers(f, ms)
	f.Required = isRequired(ms, omitempty)
	return f
}

func isRequired(ms []marker, omitempty bool) bool {
	for _, m := range ms {
		switch m.name {
		case "required", "kubebuilder:validation:Required":
			return true
		case "optional", "kubebuilder:validation:Optional":
			return false
		}
	}
	return !omitempty
}

// fillType determines the type portion of a schema node from the Go type.
func (g *generator) fillType(f *Field, t types.Type) {
	t = deref(t)

	if named, ok := t.(*types.Named); ok {
		if g.specialType(f, named) {
			return
		}
		if typeName := g.typeName(named); typeName != "" {
			if ms := g.markers.types[typeName]; len(ms) > 0 {
				applyMarkers(f, ms)
			}
		}

		if len(f.Enum) == 0 {
			if ev := g.constEnum(named); len(ev) > 0 {
				f.Enum = ev
			}
		}
		switch u := named.Underlying().(type) {
		case *types.Basic:
			g.basicType(f, u)
		case *types.Struct:
			f.Type = schemaTypeObject
			f.Properties = g.structProps(named)
		case *types.Slice:
			f.Type = "array"
			f.Items = g.elementSchema(u.Elem())
		case *types.Map:
			f.Type = schemaTypeObject
			f.Preserve = true
		default:
			f.Type = schemaTypeObject
			f.Preserve = true
		}
		return
	}

	switch u := t.(type) {
	case *types.Basic:
		g.basicType(f, u)
	case *types.Struct:
		f.Type = schemaTypeObject
		f.Properties = g.structProps(t)
	case *types.Slice:
		f.Type = "array"
		f.Items = g.elementSchema(u.Elem())
	case *types.Map:
		f.Type = schemaTypeObject
		f.Preserve = true
	case *types.Interface:
		f.Type = schemaTypeObject
		f.Preserve = true
	default:
		f.Type = schemaTypeObject
		f.Preserve = true
	}
}

func (g *generator) elementSchema(t types.Type) *Field {
	f := &Field{omitRequired: true}
	g.fillType(f, t)
	return f
}

// specialType handles well-known Kubernetes types before generic conversion.
func (g *generator) specialType(f *Field, named *types.Named) bool {
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	pkgPath := obj.Pkg().Path()
	name := obj.Name()
	switch {
	case pkgPath == "k8s.io/apimachinery/pkg/runtime" && name == "RawExtension":
		f.Type = schemaTypeObject
		f.Preserve = true
		return true
	case strings.HasSuffix(pkgPath, "apis/meta/v1") && name == "Time":
		f.Type = schemaTypeString
		f.Format = "date-time"
		return true
	case strings.HasSuffix(pkgPath, "apis/meta/v1") && name == "Duration":
		f.Type = schemaTypeString
		return true
	case strings.HasSuffix(pkgPath, "apis/meta/v1") && name == "Condition":
		f.Type = schemaTypeObject
		f.Properties = conditionProps()
		return true
	case strings.HasSuffix(pkgPath, "apis/meta/v1") &&
		(name == "ObjectMeta" || name == "TypeMeta" || name == "ListMeta"):
		f.Type = schemaTypeObject
		return true
	}
	return false
}

// conditionProps is the standard schema for metav1.Condition.
func conditionProps() map[string]*Field {
	reasonPattern := "^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$"
	typePattern := "^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?" +
		"(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$"
	min0 := int64(0)
	return map[string]*Field{
		"lastTransitionTime": {Type: schemaTypeString, Required: false, Format: "date-time"},
		"message":            {Type: schemaTypeString, Required: false, MaxLength: 32768},
		"observedGeneration": {Type: "integer", Required: false, Format: "int64", Minimum: &min0},
		"reason": {
			Type: schemaTypeString, Required: false, MinLength: 1, MaxLength: 1024, Pattern: reasonPattern,
		},
		"status": {Type: schemaTypeString, Required: false, Enum: []string{"True", "False", "Unknown"}},
		"type":   {Type: schemaTypeString, Required: false, MaxLength: 316, Pattern: typePattern},
	}
}

// basicType maps a Go basic type to a JSON schema type.
func (g *generator) basicType(f *Field, u *types.Basic) {
	switch u.Kind() {
	case types.Bool:
		f.Type = "boolean"
	case types.String:
		f.Type = schemaTypeString
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		f.Type = "integer"
		switch u.Name() {
		case "int32":
			f.Format = "int32"
		case "int64":
			f.Format = "int64"
		}
	case types.Float32, types.Float64:
		f.Type = "number"
	default:
		f.Type = schemaTypeObject
		f.Preserve = true
	}
}

func deref(t types.Type) types.Type {
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			return t
		}
		t = p.Elem()
	}
}

func (g *generator) typeName(t types.Type) string {
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}

// constEnum returns the string values of every constant declared with the
// given named string type, or nil when a type has no matching constants.
func (g *generator) constEnum(named *types.Named) []string {
	if g.enumCache == nil {
		g.enumCache = map[*types.TypeName][]string{}
	}
	tn := named.Obj()
	if v, ok := g.enumCache[tn]; ok {
		return v
	}
	var values []string
	if basic, ok := named.Underlying().(*types.Basic); ok && basic.Kind() == types.String {
		scope := g.pkg.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			c, ok := obj.(*types.Const)
			if !ok {
				continue
			}
			ct, ok := c.Type().(*types.Named)
			if !ok || ct.Obj() != tn {
				continue
			}
			values = append(values, constant.StringVal(c.Val()))
		}
		sort.Strings(values)
	}
	g.enumCache[tn] = values
	return values
}

func isK8sNamed(t types.Type, pkgSuffix, name string) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Name() == name && strings.HasSuffix(obj.Pkg().Path(), pkgSuffix)
}

// parseJSONTag extracts the relevant parts of a struct field's json tag.
func parseJSONTag(tag string) (name string, inline bool, omitempty bool, skip bool) {
	raw := reflect.StructTag(tag).Get("json")
	if raw == "-" {
		return "", false, false, true
	}
	parts := strings.Split(raw, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		switch p {
		case "omitempty":
			omitempty = true
		case "inline":
			inline = true
		}
	}
	return name, inline, omitempty, false
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
