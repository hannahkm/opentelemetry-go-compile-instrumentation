// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveFuncTag_Invalid(t *testing.T) {
	tests := []struct {
		name string
		tag  string
	}{
		{"FuncArgumentCount", "FuncArgumentCount N"},
		{"FuncReturnCount", "FuncReturnCount N"},
	}

	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			n, handled, err := resolveFuncTag(&buf, tt.tag, data)
			require.Error(t, err)
			assert.True(t, handled)
			assert.Equal(t, 0, n)
			assert.Equal(t, 0, buf.Len())
		})
	}
}

func TestResolveFuncTag_FuncName(t *testing.T) {
	tests := []struct {
		name string
		tag  string
	}{
		{"no spaces", "FuncName"},
		{"surrounding spaces", " FuncName "},
		{"trim markers", "- FuncName -"},
	}

	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			n, handled, err := resolveFuncTag(&buf, tt.tag, data)
			require.NoError(t, err)
			assert.True(t, handled)
			assert.Equal(t, "Foo", buf.String())
			assert.Equal(t, len("Foo"), n)
		})
	}
}

func TestResolveFuncTag_FuncArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(ctx int, name string) {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncArgument 0", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "ctx", buf.String())

	buf.Reset()
	_, handled, err = resolveFuncTag(&buf, "FuncArgument 1", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "name", buf.String())
}

func TestResolveFuncTag_FuncArgumentExcludesReceiver(t *testing.T) {
	funcDecl := parseFunc(t, "package main\ntype T struct{}\nfunc (t T) Foo(x int) {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncArgument 0", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "x", buf.String())
}

func TestResolveFuncTag_VariadicArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a string, b ...int) {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncArgument 0", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "a", buf.String())

	buf.Reset()
	_, handled, err = resolveFuncTag(&buf, "FuncArgument 1", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "b", buf.String())

	buf.Reset()
	_, handled, err = resolveFuncTag(&buf, "FuncArgumentCount", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "2", buf.String())
}

func TestResolveFuncTag_FuncReturn(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() (int, error) { return 0, nil }")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncReturn 0", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "_unnamedRetVal0", buf.String())

	buf.Reset()
	_, handled, err = resolveFuncTag(&buf, "FuncReturn 1", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "_unnamedRetVal1", buf.String())
}

func TestResolveFuncTag_Counts(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a, b int) (int, error) { return 0, nil }")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncArgumentCount", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "2", buf.String())

	buf.Reset()
	_, handled, err = resolveFuncTag(&buf, "FuncReturnCount", data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "2", buf.String())
}

func TestResolveFuncTag_OutOfRangeIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncArgument 1", data)
	require.Error(t, err)
	assert.True(t, handled)
	assert.Contains(t, err.Error(), "out of range")
}

func TestResolveFuncTag_NonIntegerIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncArgument x", data)
	require.Error(t, err)
	assert.True(t, handled)
	assert.Contains(t, err.Error(), "not an integer")
}

func TestResolveFuncTag_MissingIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncArgument", data)
	require.Error(t, err)
	assert.True(t, handled)
}

func TestResolveFuncTag_FuncNameExtraArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "FuncName extra", data)
	require.Error(t, err)
	assert.True(t, handled)
}

func TestResolveFuncTag_UnknownTagNotHandled(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	n, handled, err := resolveFuncTag(&buf, "SomethingElse", data)
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, buf.Len())
}

func TestResolveFuncTag_EmptyTag(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, "", data)
	require.Error(t, err)
	assert.False(t, handled)
}

func TestFuncTemplateData_FuncArgumentOfType(t *testing.T) {
	funcDecl := parseFunc(t, `package main

import "context"

func Foo(ctx context.Context, name string) {}
`)
	data := newFuncTemplateData(funcDecl)

	name, found, err := data.funcArgumentOfType("context.Context")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "ctx", name)

	name, found, err = data.funcArgumentOfType("string")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "name", name)
}

func TestFuncTemplateData_FuncArgumentOfType_PositionalIndependence(t *testing.T) {
	// The string param comes before the context.Context param; the lookup
	// must still find it by type, not by position.
	funcDecl := parseFunc(t, `package main

import "context"

func Foo(name string, ctx context.Context) {}
`)
	data := newFuncTemplateData(funcDecl)

	name, found, err := data.funcArgumentOfType("context.Context")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "ctx", name)
}

func TestFuncTemplateData_FuncArgumentOfType_NoMatch(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(name string) {}")
	data := newFuncTemplateData(funcDecl)

	_, found, err := data.funcArgumentOfType("error")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestFuncTemplateData_FuncArgumentOfType_UnsupportedParamSkipped(t *testing.T) {
	// The first parameter has a type node (func) that MatchesTypeName can't
	// reason about; it must be skipped rather than aborting the scan, so the
	// later matching string param is still found.
	funcDecl := parseFunc(t, "package main\nfunc Foo(cb func(int) int, name string) {}")
	data := newFuncTemplateData(funcDecl)

	name, found, err := data.funcArgumentOfType("string")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "name", name)
}

func TestFuncTemplateData_FuncArgumentOfType_InvalidTypeString(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(name string) {}")
	data := newFuncTemplateData(funcDecl)

	_, _, err := data.funcArgumentOfType("[]string")
	require.Error(t, err)
}

func TestFuncTemplateData_FuncArgumentOfType_UnnamedParamGetsSyntheticName(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(string) {}")
	data := newFuncTemplateData(funcDecl)

	name, found, err := data.funcArgumentOfType("string")
	require.NoError(t, err)
	assert.True(t, found)
	assert.NotEmpty(t, name)
}

func TestResolveFuncTag_PlaceholderDotNotHandled(t *testing.T) {
	// "." (used by call/decl templates) must fall through so callers can
	// apply their own handling.
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl)

	var buf bytes.Buffer
	_, handled, err := resolveFuncTag(&buf, ".", data)
	require.NoError(t, err)
	assert.False(t, handled)
}
