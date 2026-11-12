// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// formatStringCalls are the imgui calls whose first argument is a printf format string; text from the user, such as "4%", would corrupt it. Draw text with disabledText, wrappedText, bulletText, tooltipText or TextUnformatted instead.
var formatStringCalls = map[string]bool{
	"Text": true, "TextV": true, "TextDisabled": true, "TextWrapped": true, "TextColored": true, "LabelText": true,
	"BulletText": true, "SetTooltip": true, "SetItemTooltip": true, "TextDisabledV": true, "TextWrappedV": true,
}

func TestNoFormatStringCalls(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		tree, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(tree, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "imgui" && formatStringCalls[sel.Sel.Name] {
					t.Errorf("%s: imgui.%s takes a format string", fset.Position(call.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
	}
}
