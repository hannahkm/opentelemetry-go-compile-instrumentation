// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"fmt"
	"regexp"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// typeNameRe parses type-name strings of the form [*][pkg.]Name.
// It handles identifiers, qualified identifiers, and pointers to those.
// Limitations: does not handle chan, func, map, slice, or interface literals.
var typeNameRe = regexp.MustCompile(
	`\A(\*)?\s*(?:([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*)\.)?([A-Za-z_][A-Za-z0-9_]*)\z`,
)

// parsedTypeName represents a parsed Go type expression.
type parsedTypeName struct {
	importPath string // package qualifier (e.g. "context"), empty for builtins
	name       string // leaf name (e.g. "Context", "error", "int")
	pointer    bool   // whether the type is a pointer
}

// parseTypeName parses a string like "error", "int", "context.Context", or
// "*http.Request" into a parsedTypeName.
func parseTypeName(s string) (parsedTypeName, error) {
	m := typeNameRe.FindStringSubmatch(s)
	if m == nil {
		return parsedTypeName{}, ex.Newf("invalid type name %q", s)
	}
	return parsedTypeName{pointer: m[1] == "*", importPath: m[2], name: m[3]}, nil
}

// matches reports whether the dst.Expr node represents this type.
func (t parsedTypeName) matches(node dst.Expr) bool {
	matched, ok := t.matchesAny(node)
	if !ok {
		util.Unimplemented(fmt.Sprintf("signature filter: unsupported type node %T", node))
	}
	return matched
}

// matchesAny reports whether node represents this type and whether node's
// shape was recognized at all. An unsupported type node yields
// (false, false) instead of aborting the process.
//
//nolint:revive // if we add named returns then nonamedreturns will complain
func (t parsedTypeName) matchesAny(node dst.Expr) (bool, bool) {
	switch n := node.(type) {
	case *dst.Ident:
		return !t.pointer && t.importPath == n.Path && t.name == n.Name, true

	case *dst.SelectorExpr:
		ident, ok := n.X.(*dst.Ident)
		if !ok || ident.Path != "" {
			return false, true
		}
		return !t.pointer && t.importPath == ident.Name && t.name == n.Sel.Name, true

	case *dst.StarExpr:
		inner := parsedTypeName{importPath: t.importPath, name: t.name}
		matched, ok := inner.matchesAny(n.X)
		return t.pointer && matched, ok

	case *dst.IndexExpr:
		// Generic type with a single type parameter (e.g. Seq[T]).
		matched, ok := t.matchesAny(n.X)
		return !t.pointer && matched, ok

	case *dst.IndexListExpr:
		// Generic type with multiple type parameters (e.g. Map[K, V]).
		matched, ok := t.matchesAny(n.X)
		return !t.pointer && matched, ok

	case *dst.InterfaceType:
		// Only the empty interface matches "any".
		return len(n.Methods.List) == 0 && t.importPath == "" && t.name == "any", true

	default:
		return false, false
	}
}

// MatchesTypeName reports whether node represents the Go type named by
// typeStr (e.g. "error", "int", "context.Context", "*http.Request").
func MatchesTypeName(node dst.Expr, typeStr string) (bool, error) {
	tn, err := parseTypeName(typeStr)
	if err != nil {
		return false, err
	}
	matched, _ := tn.matchesAny(node)
	return matched, nil
}

// fieldListContainsType reports whether any field in fields has a type that
// matches typeStr.  Returns an error when typeStr cannot be parsed.
func fieldListContainsType(fields *dst.FieldList, typeStr string) (bool, error) {
	if fields == nil || len(fields.List) == 0 {
		return false, nil
	}
	tn, err := parseTypeName(typeStr)
	if err != nil {
		return false, err
	}
	for _, field := range fields.List {
		if tn.matches(field.Type) {
			return true, nil
		}
	}
	return false, nil
}
