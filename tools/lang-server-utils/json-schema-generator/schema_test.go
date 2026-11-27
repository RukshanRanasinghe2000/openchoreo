// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/constant"
	"go/token"
	"go/types"
	"testing"
)

func testPkgWithKind(t *testing.T) (*types.Package, *types.Named) {
	t.Helper()
	pkg := types.NewPackage("example.com/test", "test")
	tn := types.NewTypeName(token.NoPos, pkg, "MyKind", nil)
	named := types.NewNamed(tn, types.Typ[types.String], nil)
	pkg.Scope().Insert(tn)
	return pkg, named
}

func addConst(pkg *types.Package, name string, typ types.Type, val string) {
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, name, typ, constant.MakeString(val)))
}

func emptyMarkers() markerSet {
	return markerSet{fields: map[string]map[string][]marker{}, types: map[string][]marker{}}
}

func TestFillTypeNamedStringWithConstants(t *testing.T) {
	// A named string type with matching constants produces an enum.
	pkg, named := testPkgWithKind(t)
	addConst(pkg, "ValueA", named, "A")
	addConst(pkg, "ValueB", named, "B")

	g := newGenerator(pkg, emptyMarkers())
	f := &Field{}
	g.fillType(f, named)

	if f.Type != schemaTypeString {
		t.Errorf("expected type string, got %q", f.Type)
	}
	if len(f.Enum) != 2 || f.Enum[0] != "A" || f.Enum[1] != "B" {
		t.Errorf("expected enum [A B], got %v", f.Enum)
	}
}

func TestFillTypeNamedStringWithoutConstants(t *testing.T) {
	// A named string type without constants stays a plain string.
	_, named := testPkgWithKind(t)

	g := newGenerator(types.NewPackage("example.com/test", "test"), emptyMarkers())
	f := &Field{}
	g.fillType(f, named)

	if f.Type != schemaTypeString {
		t.Errorf("expected type string, got %q", f.Type)
	}
	if len(f.Enum) != 0 {
		t.Errorf("expected no enum, got %v", f.Enum)
	}
}

func TestFillTypePlainStringField(t *testing.T) {
	g := newGenerator(types.NewPackage("example.com/test", "test"), emptyMarkers())
	f := &Field{}
	g.fillType(f, types.Typ[types.String])

	if f.Type != schemaTypeString {
		t.Errorf("expected type string, got %q", f.Type)
	}
	if len(f.Enum) != 0 {
		t.Errorf("expected no enum, got %v", f.Enum)
	}
}

func TestFillTypeMultipleEnumValues(t *testing.T) {
	// All constant values are collected and sorted deterministically.
	pkg, named := testPkgWithKind(t)
	addConst(pkg, "VOne", named, "one")
	addConst(pkg, "VTwo", named, "two")
	addConst(pkg, "VThree", named, "three")

	g := newGenerator(pkg, emptyMarkers())
	f := &Field{}
	g.fillType(f, named)

	want := []string{"one", "three", "two"}
	if len(f.Enum) != len(want) {
		t.Fatalf("expected enum %v, got %v", want, f.Enum)
	}
	for i, v := range want {
		if f.Enum[i] != v {
			t.Errorf("enum[%d] = %q, want %q", i, f.Enum[i], v)
		}
	}
}

func TestFillTypeExplicitEnumMarkerWins(t *testing.T) {
	// An explicit +kubebuilder:validation:Enum marker takes precedence.
	pkg, named := testPkgWithKind(t)
	addConst(pkg, "ValueA", named, "A")
	addConst(pkg, "ValueB", named, "B")

	markers := emptyMarkers()
	markers.types["MyKind"] = []marker{{name: "kubebuilder:validation:Enum", value: "X;Y"}}

	g := newGenerator(pkg, markers)
	f := &Field{}
	g.fillType(f, named)

	want := []string{"X", "Y"}
	if len(f.Enum) != len(want) || f.Enum[0] != "X" || f.Enum[1] != "Y" {
		t.Errorf("expected enum %v from marker, got %v", want, f.Enum)
	}
}
