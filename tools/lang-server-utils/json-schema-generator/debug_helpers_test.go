// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "go/ast"

func parseCommentGroup(s string) *ast.CommentGroup {
	return &ast.CommentGroup{List: []*ast.Comment{{Text: "// " + s}}}
}
