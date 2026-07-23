// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"strconv"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"
	"github.com/valyala/fasttemplate"

	"go.opentelemetry.io/otelc/tool/ex"
	toolast "go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/util"
)

// placeholderDot is the sentinel selector name substituted for {{ . }}.
const placeholderDot = "PLACEHOLDER_0"

// callTemplate represents a code template that can be used to wrap or transform
// Go expressions. It uses fasttemplate for template execution
// and supports placeholder substitution for AST nodes.
type callTemplate struct {
	template *fasttemplate.Template
	source   string
}

// newCallTemplate creates a new callTemplate from the provided template text.
// The template text should contain {{ . }} as a placeholder for the expression
// being wrapped.
//
// Example:
//
//	newCallTemplate("wrapper({{ . }})")
func newCallTemplate(text string) (*callTemplate, error) {
	tmpl, err := fasttemplate.NewTemplate(text, "{{", "}}")
	if err != nil {
		return nil, ex.Newf("failed to parse template %s", text)
	}

	return &callTemplate{
		template: tmpl,
		source:   text,
	}, nil
}

// String returns the original template source text.
func (t *callTemplate) String() string {
	return t.source
}

// compileExpression executes the template with the given expression node as
// the placeholder value, parses the result, and returns the transformed expression.
// enclosing is the function declaration that contains node, or
// nil if node sits outside any function body (e.g. a package-level variable
// initializer); when non-nil, it makes the shared function template
// variables (FuncName, FuncArgument N, FuncReturn N, ...; see resolveFuncTag)
// available in the template alongside {{ . }}.
func (t *callTemplate) compileExpression(node dst.Expr, enclosing *dst.FuncDecl) (dst.Expr, error) {
	return t.compile(node, enclosing, false)
}

// compileCall also enables the wrap_call-only template variables
// {{ FuncArgumentOfType <type> }}, {{ CallArgument N }}, and {{ CallArgumentCount }}.
func (t *callTemplate) compileCall(call *dst.CallExpr, enclosing *dst.FuncDecl) (dst.Expr, error) {
	return t.compile(call, enclosing, true)
}

// The process:
//  1. Execute the template with fixed placeholder strings (_.PLACEHOLDER_0
//     for {{ . }}, _.PLACEHOLDER_ARG_N for {{ CallArgument N }})
//  2. Wrap the result in a minimal function and parse it
//  3. Extract the expression from the parsed function
//  4. Replace the placeholders with the actual AST nodes
//
// enableCallTags is true only for compileCall, whose node is always the
// matched *dst.CallExpr itself; it makes the wrap_call-only tags available.
func (t *callTemplate) compile(node dst.Expr, enclosing *dst.FuncDecl, enableCallTags bool) (dst.Expr, error) {
	var funcData *funcTemplateData
	if enclosing != nil {
		funcData = newFuncTemplateData(enclosing)
	}

	var call *dst.CallExpr
	if enableCallTags {
		call = util.AssertType[*dst.CallExpr](node)
	}

	placeholders := make(map[string]dst.Node)

	// Execute the user's template with fixed placeholder strings. The
	// TagFunc handles {{ . }}, {{.}}, and {{- . -}} variants by normalizing
	// the tag content before matching, and delegates the wrap_call-only tags
	// (FuncArgumentOfType, CallArgument*) and the shared Func* tags to their
	// resolvers before falling back to the "." placeholder. The "." branch
	// registers node into placeholders lazily, just like CallArgument N
	// registers its own entry in resolveCallTag, so a template that never
	// references "." never requires node to appear in the output.
	userResult, err := t.template.ExecuteFuncStringWithErr(func(w io.Writer, tag string) (int, error) {
		if call != nil {
			if n, handled, resolveErr := resolveCallTag(w, tag, funcData, call, placeholders); handled {
				return n, resolveErr
			}
		}

		if n, handled, resolveErr := resolveFuncTag(w, tag, funcData); handled {
			return n, resolveErr
		}

		// Trim spaces and optional trim markers (e.g. {{- . -}})
		if fields := cleanTagFields(tag); len(fields) == 1 && fields[0] == "." {
			placeholders[placeholderDot] = node
			return io.WriteString(w, "_."+placeholderDot)
		}
		return 0, ex.Newf(
			"unknown template tag %q; only {{ . }}, the function template variables, "+
				"and (for wrap_call) FuncArgumentOfType/CallArgument are supported", tag,
		)
	})
	if err != nil {
		return nil, ex.Wrapf(err, "failed to execute template")
	}

	// Wrap the result in a minimal function so we can parse it as Go code.
	wrapped := "package _\nfunc _() {\n\t" + userResult + "\n}\n"

	// Parse the wrapped code
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", []byte(wrapped), parser.ParseComments)
	if err != nil {
		// Format the error with the generated code for debugging
		formatted, _ := format.Source([]byte(wrapped))
		return nil, ex.Wrapf(err, "failed to parse generated code\nGenerated code:\n%s", formatted)
	}

	// Convert ast.File to dst.File
	dec := decorator.NewDecorator(fset)
	dstFile, err := dec.DecorateFile(file)
	if err != nil {
		return nil, ex.Newf("failed to decorate AST")
	}

	// Extract the expression from the function body
	if len(dstFile.Decls) == 0 {
		return nil, ex.New("no declarations found in generated code")
	}

	funcDecl, ok := dstFile.Decls[0].(*dst.FuncDecl)
	if !ok {
		return nil, ex.Newf("expected function declaration, got %T", dstFile.Decls[0])
	}

	if funcDecl.Body == nil || len(funcDecl.Body.List) == 0 {
		return nil, ex.New("function body is empty")
	}
	if len(funcDecl.Body.List) != 1 {
		return nil, ex.Newf("expected single expression statement, got %d statements", len(funcDecl.Body.List))
	}

	exprStmt, ok := funcDecl.Body.List[0].(*dst.ExprStmt)
	if !ok {
		return nil, ex.Newf("expected expression statement, got %T", funcDecl.Body.List[0])
	}

	if len(placeholders) == 0 {
		return nil, ex.New(
			"template does not reference any placeholder " +
				"(e.g. {{ . }}, FuncArgument N, FuncArgumentOfType <type>, CallArgument N)",
		)
	}

	// Replace placeholders with the actual nodes.
	result, replacedKeys := replacePlaceholders(exprStmt.X, placeholders)
	for key := range placeholders {
		if !replacedKeys[key] {
			return nil, ex.Newf("template output did not contain expected placeholder %q", key)
		}
	}

	resultExpr, ok := result.(dst.Expr)
	if !ok {
		return nil, ex.New("placeholder replacement didn't produce an expression")
	}

	return resultExpr, nil
}

// isCallTagVerb reports whether `verb` names one of the wrap_call-only
// template variables.
func isCallTagVerb(verb string) bool {
	switch verb {
	case "FuncArgumentOfType", "CallArgument", "CallArgumentCount":
		return true
	default:
		return false
	}
}

// resolveCallTag attempts to resolve a fasttemplate tag as one of the
// wrap_call-only template variables: {{ FuncArgumentOfType <type> }} (a
// parameter of the enclosing function matched by declared type),
// {{ CallArgument N }} (the N-th argument of the matched call), and
// {{ CallArgumentCount }}. The tag is trimmed of surrounding whitespace and
// "-" trim markers (e.g. "{{- CallArgument 0 -}}") before matching.
//
// A resolved CallArgument N registers a decoration-stripped clone of
// call.Args[N] and writes the matching "_.PLACEHOLDER_ARG_N" sentinel,
// stripping its decorations (comments, blank lines, etc).
// A replace template that also references {{ . }} keeps the matched call,
// comments and all, intact at that other position.
func resolveCallTag(
	w io.Writer, tag string, funcData *funcTemplateData, call *dst.CallExpr, placeholders map[string]dst.Node,
) (int, bool, error) {
	fields := cleanTagFields(tag)
	if len(fields) == 0 || !isCallTagVerb(fields[0]) {
		return 0, false, nil
	}

	switch fields[0] {
	case "FuncArgumentOfType":
		if len(fields) != numTagFields {
			return 0, true, ex.Newf(
				"invalid template tag %q: FuncArgumentOfType requires exactly one type argument", tag,
			)
		}
		if funcData == nil {
			return 0, true, ex.Newf(
				"invalid template tag %q: no enclosing function is available at this position", tag,
			)
		}
		name, found, err := funcData.funcArgumentOfType(fields[1])
		if err != nil {
			return 0, true, ex.Wrapf(err, "invalid template tag %q", tag)
		}
		if !found {
			return 0, true, ex.Newf("invalid template tag %q: no parameter of type %q found", tag, fields[1])
		}
		n, writeErr := io.WriteString(w, name)
		return n, true, writeErr

	case "CallArgument":
		if len(fields) != numTagFields {
			return 0, true, ex.Newf(
				"invalid template tag %q: CallArgument requires exactly one index argument", tag,
			)
		}
		idx, convErr := strconv.Atoi(fields[1])
		if convErr != nil {
			return 0, true, ex.Newf(
				"invalid template tag %q: CallArgument index %q is not an integer", tag, fields[1],
			)
		}
		if idx < 0 || idx >= len(call.Args) {
			return 0, true, ex.Newf(
				"invalid template tag %q: CallArgument index %d out of range [0, %d)", tag, idx, len(call.Args),
			)
		}
		key := "PLACEHOLDER_ARG_" + strconv.Itoa(idx)
		argClone := dst.Clone(call.Args[idx])
		stripDecorations(argClone)
		placeholders[key] = argClone
		n, writeErr := io.WriteString(w, "_."+key)
		return n, true, writeErr

	case "CallArgumentCount":
		if len(fields) != 1 {
			return 0, true, ex.Newf("invalid template tag %q: CallArgumentCount takes no argument", tag)
		}
		n, writeErr := io.WriteString(w, strconv.Itoa(len(call.Args)))
		return n, true, writeErr

	default:
		return 0, false, nil
	}
}

// stripDecorations recursively clears every comment and blank-line
// decoration from node and its descendants.
func stripDecorations(node dst.Node) {
	dstutil.Apply(node, func(cursor *dstutil.Cursor) bool {
		n := cursor.Node()
		if n == nil {
			return true
		}
		decs := n.Decorations()
		decs.Before = dst.None
		decs.After = dst.None
		decs.Start.Clear()
		decs.End.Clear()
		return true
	}, nil)
}

// parseGoExpression parses a Go expression string into a dst.Expr.
func parseGoExpression(expr string) (dst.Expr, error) {
	funcDecl, err := parseSnippetFuncDecl("package _\nfunc _() {\n\t"+expr+"\n}\n", expr)
	if err != nil {
		return nil, err
	}
	exprStmt, ok := funcDecl.Body.List[0].(*dst.ExprStmt)
	if !ok {
		return nil, ex.Newf(
			"expression %q did not parse as an expression statement (got %T)",
			expr, funcDecl.Body.List[0])
	}
	return exprStmt.X, nil
}

// parseGoTypeExpression parses a Go type string (e.g. "grpc.DialOption") into a dst.Expr.
func parseGoTypeExpression(typeStr string) (dst.Expr, error) {
	funcDecl, err := parseSnippetFuncDecl("package _\nfunc _() {\n\tvar _ "+typeStr+"\n}\n", typeStr)
	if err != nil {
		return nil, err
	}
	declStmt, ok := funcDecl.Body.List[0].(*dst.DeclStmt)
	if !ok {
		return nil, ex.Newf(
			"type %q did not parse as a declaration statement (got %T)",
			typeStr, funcDecl.Body.List[0])
	}
	genDecl, ok := declStmt.Decl.(*dst.GenDecl)
	if !ok || len(genDecl.Specs) == 0 {
		return nil, ex.Newf("unexpected declaration shape for type %q", typeStr)
	}
	valueSpec, ok := genDecl.Specs[0].(*dst.ValueSpec)
	if !ok || valueSpec.Type == nil {
		return nil, ex.Newf("unexpected spec shape for type %q", typeStr)
	}
	return valueSpec.Type, nil
}

// parseSnippetFuncDecl parses a minimal Go source snippet of the form
// "package _\nfunc _() { <body> }\n" and returns the function declaration.
// label is used in error messages to identify the original snippet.
func parseSnippetFuncDecl(src, label string) (*dst.FuncDecl, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", []byte(src), parser.ParseComments)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to parse %q", label)
	}
	dec := decorator.NewDecorator(fset)
	dstFile, err := dec.DecorateFile(file)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to decorate AST for %q", label)
	}
	if len(dstFile.Decls) == 0 {
		return nil, ex.Newf("no declarations found for %q", label)
	}
	funcDecl, ok := dstFile.Decls[0].(*dst.FuncDecl)
	if !ok || funcDecl.Body == nil || len(funcDecl.Body.List) == 0 {
		return nil, ex.Newf("unexpected AST shape for %q", label)
	}
	return funcDecl, nil
}

// replacePlaceholders replaces every "_.PLACEHOLDER_*" selector expression in
// node with its corresponding entry in placeholders.
// Returns the resulting node along with which keys were actually found and replaced.
//
// Each occurrence gets its own dst.Clone of the registered node: a template
// may reference the same placeholder (e.g. "{{ CallArgument 0 }}") more than
// once.
func replacePlaceholders(node dst.Node, placeholders map[string]dst.Node) (dst.Node, map[string]bool) {
	replaced := make(map[string]bool, len(placeholders))
	result := dstutil.Apply(
		node,
		func(cursor *dstutil.Cursor) bool {
			selectorExpr, ok := cursor.Node().(*dst.SelectorExpr)
			if !ok {
				return true
			}

			ident, ok := selectorExpr.X.(*dst.Ident)
			if !ok || ident.Name != toolast.IdentIgnore {
				return true
			}

			replacement, ok := placeholders[selectorExpr.Sel.Name]
			if !ok {
				return true
			}

			cursor.Replace(dst.Clone(replacement))
			replaced[selectorExpr.Sel.Name] = true
			return false
		},
		nil,
	)
	return result, replaced
}
