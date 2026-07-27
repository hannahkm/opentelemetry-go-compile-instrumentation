// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCallTemplate_Success(t *testing.T) {
	text := "wrapper({{ . }})"

	tmpl, err := newCallTemplate(text)

	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, text, tmpl.String())
}

func TestNewCallTemplate_InvalidSyntax(t *testing.T) {
	text := "wrapper({{ .Field )" // Invalid template syntax - missing closing }}

	tmpl, err := newCallTemplate(text)

	require.Error(t, err)
	assert.Nil(t, tmpl)
	assert.Contains(t, err.Error(), "failed to parse template")
}

func TestNewCallTemplate_EmptyTemplate(t *testing.T) {
	text := ""

	tmpl, err := newCallTemplate(text)

	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, text, tmpl.String())
}

func TestCompileExpression_FuncArgumentWithEnclosingFunc(t *testing.T) {
	tmpl, err := newCallTemplate("traced({{ .FuncArgument 0 }}, {{ . }})")
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\nfunc Handler(name string) {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	nameArg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "name", nameArg.Name)
}

func TestCompileExpression_FuncTagWithoutEnclosingFuncErrors(t *testing.T) {
	tmpl, err := newCallTemplate("traced({{ .FuncName }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	_, err = tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no enclosing function is available")
}

func TestCompileExpression_FuncArgumentOfType_Found(t *testing.T) {
	tmpl, err := newCallTemplate(`traced({{ .FuncArgumentOfType "context.Context" }}, {{ . }})`)
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\nfunc Handler(ctx context.Context, name string) {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	argIdent, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "ctx", argIdent.Name)
}

func TestCompileExpression_FuncArgumentOfType_NotFound(t *testing.T) {
	tmpl, err := newCallTemplate(`wrap("{{ .FuncArgumentOfType "io.Reader" }}", {{ . }})`)
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\nfunc Handler(ctx context.Context, name string) {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	lit, ok := resultCall.Args[0].(*dst.BasicLit)
	require.True(t, ok, "expected *dst.BasicLit, got %T", resultCall.Args[0])
	assert.Equal(t, `""`, lit.Value)
}

func TestCompileExpression_FuncArgumentOfType_SkipsUnsupportedParamTypes(t *testing.T) {
	tmpl, err := newCallTemplate(`traced({{ .FuncArgumentOfType "context.Context" }}, {{ . }})`)
	require.NoError(t, err)

	// data []byte has a type shape (slice) that MatchesTypeName cannot
	// compare against a plain type-name filter
	enclosing := parseFunc(t, "package main\nfunc Handler(data []byte, ctx context.Context) {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	argIdent, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "ctx", argIdent.Name)
}

func TestCompileExpression_FuncArgumentOfType_NoEnclosingFuncErrors(t *testing.T) {
	tmpl, err := newCallTemplate(`traced({{ .FuncArgumentOfType "context.Context" }})`)
	require.NoError(t, err)

	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	_, err = tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no enclosing function is available")
}

func TestCompileExpression_CallArgumentIsWrappedCallNotEnclosingFunc(t *testing.T) {
	tmpl, err := newCallTemplate("traced({{ .CallArgument 0 }}, {{ .FuncArgument 0 }}, {{ . }})")
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\nfunc Handler(outerParam string) {}")
	originalCall := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "getValue"},
		Args: []dst.Expr{&dst.Ident{Name: "innerArg"}},
	}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 3)

	callArg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "innerArg", callArg.Name, "CallArgument must resolve to the wrapped call's own argument")

	funcArg, ok := resultCall.Args[1].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[1])
	assert.Equal(t, "outerParam", funcArg.Name, "FuncArgument must resolve to the enclosing function's parameter")
}

func TestCompileExpression_CallArgumentCount(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgumentCount }}, {{ . }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "getValue"},
		Args: []dst.Expr{&dst.Ident{Name: "a"}, &dst.Ident{Name: "b"}},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	countLit, ok := resultCall.Args[0].(*dst.BasicLit)
	require.True(t, ok, "expected *dst.BasicLit, got %T", resultCall.Args[0])
	assert.Equal(t, "2", countLit.Value)
}

func TestCompileExpression_CallArgumentOutOfRange(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgument 5 }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "f"},
		Args: []dst.Expr{&dst.Ident{Name: "a"}},
	}

	_, err = tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestCompileExpression_CallArgumentUnwrapsParens(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgument 0 }}, {{ .CallArgumentCount }}, {{ . }})")
	require.NoError(t, err)

	call := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "f"},
		Args: []dst.Expr{&dst.Ident{Name: "a"}},
	}
	parenthesized := &dst.ParenExpr{X: call}

	result, err := tmpl.compileExpression(parenthesized, nil)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 3)
	arg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "a", arg.Name)
	countLit, ok := resultCall.Args[1].(*dst.BasicLit)
	require.True(t, ok, "expected *dst.BasicLit, got %T", resultCall.Args[1])
	assert.Equal(t, "1", countLit.Value)
}

func TestCompileExpression_CallArgumentRequiresCallExpr(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgument 0 }})")
	require.NoError(t, err)

	nonCall := &dst.BasicLit{Kind: token.INT, Value: "5"}

	_, err = tmpl.compileExpression(nonCall, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "function call")
}

func TestCompileExpression_CallArgumentComplexExpression(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgument 0 }}, {{ . }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "f"},
		Args: []dst.Expr{
			&dst.BinaryExpr{X: &dst.Ident{Name: "a"}, Op: token.ADD, Y: &dst.Ident{Name: "b"}},
		},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	arg, ok := resultCall.Args[0].(*dst.BinaryExpr)
	require.True(t, ok, "expected *dst.BinaryExpr, got %T", resultCall.Args[0])
	xIdent, ok := arg.X.(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", arg.X)
	assert.Equal(t, "a", xIdent.Name)
}

func TestCompileExpression_SimpleWrapping(t *testing.T) {
	tmpl, err := newCallTemplate("wrapper({{ . }})")
	require.NoError(t, err)

	// Create a simple call expression: funcCall()
	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "funcCall"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)

	// Verify the outer wrapper function
	wrapperIdent, ok := resultCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "wrapper", wrapperIdent.Name)

	// Verify the original call is inside
	require.Len(t, resultCall.Args, 1)
	innerCall, ok := resultCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	innerIdent, ok := innerCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "funcCall", innerIdent.Name)
}

func TestCompileExpression_IIFE(t *testing.T) {
	tmpl, err := newCallTemplate("(func() int { return {{ . }} })()")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression (the IIFE invocation)
	_, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr for IIFE, got %T", result)
}

func TestCompileExpression_MultiplePlaceholders(t *testing.T) {
	// Template with multiple {{ . }} occurrences
	tmpl, err := newCallTemplate("combine({{ . }}, {{ . }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)

	// Verify both arguments are present
	assert.Len(t, resultCall.Args, 2)
}

func TestCompileExpression_InvalidGoSyntax(t *testing.T) {
	// Template that parses fine but produces invalid Go syntax
	tmpl, err := newCallTemplate("func {{ . }}") // "func" keyword without proper syntax
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse generated code")
}

func TestCompileExpression_ComplexNestedExpression(t *testing.T) {
	tmpl, err := newCallTemplate("outer(middle({{ . }}))")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "inner"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify nested structure: outer(middle(inner()))
	outerCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "outer", outerCall.Fun.(*dst.Ident).Name)

	require.Len(t, outerCall.Args, 1)
	middleCall, ok := outerCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "middle", middleCall.Fun.(*dst.Ident).Name)

	require.Len(t, middleCall.Args, 1)
	innerCall, ok := middleCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "inner", innerCall.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_WithBinaryExpression(t *testing.T) {
	tmpl, err := newCallTemplate("{{ . }} + 1")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a binary expression
	binaryExpr, ok := result.(*dst.BinaryExpr)
	require.True(t, ok, "expected *dst.BinaryExpr, got %T", result)

	// Verify the left side is our call
	leftCall, ok := binaryExpr.X.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "getValue", leftCall.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_SelectorExpression(t *testing.T) {
	tmpl, err := newCallTemplate("{{ . }}.Field")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getStruct"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a selector expression
	selExpr, ok := result.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", result)
	assert.Equal(t, "Field", selExpr.Sel.Name)

	// Verify X is our call
	call, ok := selExpr.X.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "getStruct", call.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_EmptyResult(t *testing.T) {
	// Template that produces nothing (empty expression)
	tmpl, err := newCallTemplate("")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	// Should error because the function body is empty
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "function body is empty")
}

func TestCompileExpression_PlaceholderNotReplaced(t *testing.T) {
	tmpl, err := newCallTemplate(`wrapper("{{ . }}")`)
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "placeholder")
}

func TestCompileExpression_MultipleStatements(t *testing.T) {
	tmpl, err := newCallTemplate("first(); {{ . }}")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "single expression statement")
}

func TestCompileExpression_NonExpressionStatement(t *testing.T) {
	// Template that produces a non-expression statement
	// This is tricky - we need something that parses as a statement but not as an expression
	tmpl, err := newCallTemplate("return")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	// Should error because it's not an expression statement
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "expected expression statement")
}
