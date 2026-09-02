//go:build !js

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// A player handle must outlive the call that makes it. oto attaches a
// runtime.AddCleanup to every *oto.Player that closes it once the handle
// becomes unreachable (internal/mux.Mux.NewPlayer); the mux itself keeps only
// the inner playerImpl, so nothing else roots it. Left in a local, a player
// stops at whatever GC first notices — permanently, and silently, because
// Close returns the player's nil error. It cost a "the game loses audio after
// a while" bug that reproduced nowhere else, because every host/ front end
// happened to store its player in a field and only xsofymod did not.
//
// The rule this pins: the result of NewPlayer reaches a field, or the function
// keeps it alive explicitly. Files are parsed directly rather than type-checked,
// so js-only front ends are covered by a test that runs natively — which is the
// point, since the one that broke was the js one.
func TestNewPlayerResultOutlivesItsScope(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	var checked int
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "dist" || n == "node_modules" || strings.HasPrefix(n, ".") && n != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || !callsNewPlayer(as.Rhs) {
					return true
				}
				checked++
				for _, lhs := range as.Lhs {
					switch v := lhs.(type) {
					case *ast.SelectorExpr: // m.player = ... — rooted by the struct
						return true
					case *ast.Ident:
						if v.Name == "_" {
							continue
						}
						if rooted(fn.Body, v.Name) {
							return true
						}
						t.Errorf("%s: %s holds the result of NewPlayer in a local that never "+
							"reaches a field and is never runtime.KeepAlive'd — oto's cleanup "+
							"closes it at the next GC and audio stops with no error. Assign it "+
							"to a struct field or keep it alive explicitly.",
							rel, v.Name)
					}
				}
				return true
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("found no NewPlayer call sites — the guard is not looking where the code is")
	}
	t.Logf("checked %d NewPlayer call site(s)", checked)
}

func callsNewPlayer(exprs []ast.Expr) bool {
	found := false
	for _, e := range exprs {
		ast.Inspect(e, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewPlayer" {
					found = true
				}
			}
			return true
		})
	}
	return found
}

// rooted reports whether name escapes the function body: stored into a field
// (m.player = player), or pinned with runtime.KeepAlive(player).
func rooted(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "KeepAlive" || len(v.Args) != 1 {
				return true
			}
			if id, ok := v.Args[0].(*ast.Ident); ok && id.Name == name {
				found = true
			}
		case *ast.AssignStmt:
			toField := false
			for _, l := range v.Lhs {
				if _, ok := l.(*ast.SelectorExpr); ok {
					toField = true
				}
			}
			if toField && mentions(v.Rhs, name) {
				found = true
			}
		}
		return true
	})
	return found
}

func mentions(exprs []ast.Expr, name string) bool {
	found := false
	for _, e := range exprs {
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == name {
				found = true
			}
			return true
		})
	}
	return found
}
