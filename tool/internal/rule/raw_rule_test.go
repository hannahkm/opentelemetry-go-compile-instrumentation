// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewInstRawRule(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		ruleName    string
		expectError bool
	}{
		{
			name: "valid raw",
			yamlContent: `
func: Bar
target: main
raw: "_ = 0"
`,
			ruleName:    "test-raw",
			expectError: false,
		},
		{
			name: "valid raw with template tag",
			yamlContent: `
func: Bar
target: main
raw: "println({{ FuncArgument 0 }})"
`,
			ruleName:    "templated-raw",
			expectError: false,
		},
		{
			name: "empty raw",
			yamlContent: `
func: Bar
target: main
raw: ""
`,
			ruleName:    "empty-raw",
			expectError: true,
		},
		{
			name: "invalid template syntax in raw",
			yamlContent: `
func: Bar
target: main
raw: "println({{ FuncArgument 0 )"
`,
			ruleName:    "bad-template-raw",
			expectError: true,
		},
		{
			name: "invalid pattern",
			yamlContent: `
func: Bar
target: main
raw: "_ = 0"
pattern: "["
`,
			ruleName:    "bad-pattern",
			expectError: true,
		},
		{
			name: "invalid placement",
			yamlContent: `
func: Bar
target: main
raw: "_ = 0"
placement: "sideways"
`,
			ruleName:    "bad-placement",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields map[string]any
			err := yaml.Unmarshal([]byte(tt.yamlContent), &fields)
			require.NoError(t, err)

			data, err := yaml.Marshal(fields)
			require.NoError(t, err)

			r, err := NewInstRawRule(data, tt.ruleName)
			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, r)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.Equal(t, tt.ruleName, r.GetName())
		})
	}
}
