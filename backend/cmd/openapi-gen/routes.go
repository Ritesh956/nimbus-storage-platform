package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Route is one registered endpoint, extracted by parsing cmd/api/*.go's
// source rather than executing it — this is the part of this generator
// that can genuinely never drift from what's actually registered: it reads
// the exact same string literals mux.Handle/mux.HandleFunc build the real
// router from. Schema richness (routeSchemas in main.go) is a separate,
// hand-maintained mapping and can go stale; the path/method/auth surface
// extracted here cannot, short of the Go source itself lying about what
// string it passes to mux.Handle — which would be a compile-time-checkable
// typo at worst, not a silent drift.
type Route struct {
	Method string
	Path   string
	// Auth is the set of middleware-wrapper identifiers found wrapping the
	// handler (requireAuth, requireMember, requirePlatformAdmin, ...) — used
	// to decide whether the operation needs the bearerAuth security
	// requirement. A route with no wrappers (e.g. the public share-resolve
	// routes) is intentionally public — see wire_upload.go's own comment on
	// why those specific routes skip requireAuth.
	Auth []string
	File string
	Line int
}

// authMarkers are every requireX-shaped middleware wrapper this codebase's
// wire_*.go files actually use to gate a route. Anything not in this list
// (loginLimiter, the rate-limit middleware) isn't an auth requirement, so
// it's deliberately excluded here.
var authMarkers = map[string]bool{
	"requireAuth":          true,
	"requireMember":        true,
	"requireOrgAdmin":      true,
	"requirePlatformAdmin": true,
	"requireFolderAccess":  true,
	"requireFileAccess":    true,
	"requireUploadAccess":  true,
}

// parseRoutes walks every non-test .go file directly under dir (cmd/api)
// for mux.Handle(...)/mux.HandleFunc(...) calls and extracts (method, path,
// auth wrappers) from the literal arguments — no execution, just source
// parsing, so it works without needing a live Postgres/Redis/NATS to
// construct the real mux.
func parseRoutes(dir string) ([]Route, error) {
	fset := token.NewFileSet()
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}

	var routes []Route
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		base := filepath.Base(path)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || recv.Name != "mux" {
				return true
			}
			if sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
				return true
			}
			if len(call.Args) < 2 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			pattern, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			method, urlPath, found := strings.Cut(pattern, " ")
			if !found {
				return true
			}

			var auth []string
			ast.Inspect(call.Args[1], func(inner ast.Node) bool {
				ident, ok := inner.(*ast.Ident)
				if !ok {
					return true
				}
				if authMarkers[ident.Name] {
					auth = append(auth, ident.Name)
				}
				return true
			})

			pos := fset.Position(call.Pos())
			routes = append(routes, Route{
				Method: method,
				Path:   urlPath,
				Auth:   auth,
				File:   base,
				Line:   pos.Line,
			})
			return true
		})
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	return routes, nil
}

// openAPIPath converts Go 1.22 mux pattern params ({orgId}) to OpenAPI's
// {orgId} form — they're already identical, so this only strips the method
// prefix that's already been split off by the caller. Kept as a named
// function so the (currently trivial) conversion has one place to grow if
// the mux pattern syntax and OpenAPI's ever diverge.
func openAPIPath(pattern string) string {
	return pattern
}
