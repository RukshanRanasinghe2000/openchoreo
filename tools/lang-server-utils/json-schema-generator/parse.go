// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// parseAPI loads the v1alpha1 API package and type-checks it. Kubernetes
// imports are supplied by a fabricated importer so the whole OpenChoreo module
// need not be built.
func parseAPI(apiDir string) (*types.Package, []*ast.File, []error, error) {
	fset := token.NewFileSet()
	names, err := apiTypeFiles(apiDir)
	if err != nil {
		return nil, nil, nil, err
	}
	var files []*ast.File
	for _, name := range names {
		path := filepath.Join(apiDir, name)
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, nil, err
		}
		files = append(files, f)
	}

	var typeErrs []error
	conf := &types.Config{
		Importer: &fakeImporter{},
		Error: func(err error) {
			typeErrs = append(typeErrs, err)
		},
	}
	pkg, err := conf.Check("v1alpha1", fset, files, nil)
	if err != nil {
		typeErrs = append(typeErrs, err)
	}
	return pkg, files, typeErrs, nil
}

// apiTypeFiles returns the *_types.go files, excluding generated deepcopy code,
// tests, and the group-version registration file.
func apiTypeFiles(apiDir string) ([]string, error) {
	entries, err := os.ReadDir(apiDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, "_types.go") && name != "types.go" {
			continue
		}
		if name == "groupversion_info.go" ||
			name == "zz_generated.deepcopy.go" ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// fakeImporter supplies minimal definitions for the Kubernetes packages the
// API types import, and delegates everything else to the default importer.
type fakeImporter struct{}

func (f *fakeImporter) Import(path string) (*types.Package, error) {
	if pkg := fabricatePackage(path); pkg != nil {
		pkg.MarkComplete()
		return pkg, nil
	}
	return importer.Default().Import(path)
}

func fabricatePackage(path string) *types.Package {
	switch path {
	case "k8s.io/apimachinery/pkg/apis/meta/v1":
		return fabricateMetaV1()
	case "k8s.io/apimachinery/pkg/runtime":
		return fabricateRuntime()
	case "k8s.io/api/core/v1":
		return fabricateCoreV1()
	}
	return nil
}

func newPkg(path, name string) *types.Package {
	return types.NewPackage(path, name)
}

func newNamed(pkg *types.Package, name string, underlying types.Type) *types.Named {
	obj := types.NewTypeName(token.NoPos, pkg, name, nil)
	return types.NewNamed(obj, underlying, nil)
}

func newField(pkg *types.Package, name, tag string, typ types.Type) *types.Var {
	return types.NewField(token.NoPos, pkg, name, typ, false)
}

func newStruct(pkg *types.Package, name string, fields []*types.Var) *types.Named {
	tags := make([]string, len(fields))
	return newNamed(pkg, name, types.NewStruct(fields, tags))
}

func fabricateMetaV1() *types.Package {
	pkg := newPkg("k8s.io/apimachinery/pkg/apis/meta/v1", "v1")
	str := types.Typ[types.String]
	i64 := types.Typ[types.Int64]
	strMap := types.NewMap(str, str)
	timeT := newStruct(pkg, "Time", nil)

	insert := []*types.Named{
		timeT,
		newStruct(pkg, "Duration", nil),
		newStruct(pkg, "TypeMeta", []*types.Var{
			newField(pkg, "Kind", `json:"kind,omitempty"`, str),
			newField(pkg, "APIVersion", `json:"apiVersion,omitempty"`, str),
		}),
		newStruct(pkg, "ObjectMeta", []*types.Var{
			newField(pkg, "Name", `json:"name,omitempty"`, str),
			newField(pkg, "Namespace", `json:"namespace,omitempty"`, str),
			newField(pkg, "UID", `json:"uid,omitempty"`, str),
			newField(pkg, "CreationTimestamp", `json:"creationTimestamp,omitempty"`, timeT),
			newField(pkg, "Labels", `json:"labels,omitempty"`, strMap),
			newField(pkg, "Annotations", `json:"annotations,omitempty"`, strMap),
		}),
		newStruct(pkg, "Condition", []*types.Var{
			newField(pkg, "Type", `json:"type"`, str),
			newField(pkg, "Status", `json:"status"`, str),
			newField(pkg, "ObservedGeneration", `json:"observedGeneration,omitempty"`, i64),
			newField(pkg, "LastTransitionTime", `json:"lastTransitionTime"`, timeT),
			newField(pkg, "Reason", `json:"reason"`, str),
			newField(pkg, "Message", `json:"message"`, str),
		}),
		newStruct(pkg, "ListMeta", []*types.Var{
			newField(pkg, "ResourceVersion", `json:"resourceVersion,omitempty"`, str),
		}),
	}
	insertNamed(pkg, insert...)
	return pkg
}

func fabricateRuntime() *types.Package {
	pkg := newPkg("k8s.io/apimachinery/pkg/runtime", "runtime")
	objectT := newNamed(pkg, "Object", types.NewInterfaceType(nil, nil))
	objectT.Underlying().(*types.Interface).Complete()
	bytesT := types.NewSlice(types.Typ[types.Byte])
	emptyIface := types.NewInterfaceType(nil, nil).Complete()
	rawExt := newStruct(pkg, "RawExtension", []*types.Var{
		newField(pkg, "Raw", `json:"raw,omitempty"`, bytesT),
		newField(pkg, "Object", `json:"-"`, emptyIface),
	})
	insertNamed(pkg, objectT, rawExt)
	return pkg
}

func fabricateCoreV1() *types.Package {
	pkg := newPkg("k8s.io/api/core/v1", "v1")
	insertNamed(pkg, newNamed(pkg, "SecretType", types.Typ[types.String]))
	return pkg
}

func insertNamed(pkg *types.Package, named ...*types.Named) {
	for _, n := range named {
		pkg.Scope().Insert(n.Obj())
	}
}
