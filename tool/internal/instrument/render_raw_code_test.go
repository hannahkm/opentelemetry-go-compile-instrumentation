// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderRawCode(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		raw      string
		expected string
	}{
		{
			name:     "no braces left untouched",
			src:      "package main\nfunc Foo(a int) {}",
			raw:      `println("static")`,
			expected: `println("static")`,
		},
		{
			name:     "FuncName",
			src:      "package main\nfunc Foo() {}",
			raw:      "call({{FuncName}})",
			expected: "call(Foo)",
		},
		{
			name:     "FuncArgument",
			src:      "package main\nfunc Foo(ctx int, name string) {}",
			raw:      "use({{ FuncArgument 0 }}, {{ FuncArgument 1 }})",
			expected: "use(ctx, name)",
		},
		{
			name:     "FuncReturn",
			src:      "package main\nfunc Foo() (int, error) { return 0, nil }",
			raw:      "check({{ FuncReturn 0 }}, {{ FuncReturn 1 }})",
			expected: "check(_unnamedRetVal0, _unnamedRetVal1)",
		},
		{
			name:     "counts",
			src:      "package main\nfunc Foo(a, b int) (int, error) { return 0, nil }",
			raw:      "n={{FuncArgumentCount}} m={{FuncReturnCount}}",
			expected: "n=2 m=2",
		},
		{
			name:     "nested composite literal left untouched",
			src:      "package main\nfunc Foo() {}",
			raw:      `x := [][]int{{1, 2}, {3, 4}}`,
			expected: `x := [][]int{{1, 2}, {3, 4}}`,
		},
		{
			name:     "nested composite literal alongside a real placeholder",
			src:      "package main\nfunc Foo() {}",
			raw:      `attrs := []Point{{X: 1, Y: 2}}; call({{FuncName}})`,
			expected: `attrs := []Point{{X: 1, Y: 2}}; call(Foo)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcDecl := parseFunc(t, tt.src)

			result, err := renderRawCode(tt.raw, funcDecl)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderRawCode_UnrecognizedTagLeftUntouched(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	result, err := renderRawCode("{{Foo}}", funcDecl)

	require.NoError(t, err)
	assert.Equal(t, "{{Foo}}", result)
}

func TestRenderRawCode_OutOfRangeArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{FuncArgument 0}}", funcDecl)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestRenderRawCode_NonIntegerArgumentIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")

	_, err := renderRawCode("{{FuncArgument abc}}", funcDecl)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an integer")
}

func TestRenderRawCode_InvalidTemplateSyntax(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{FuncName", funcDecl)

	require.Error(t, err)
}
