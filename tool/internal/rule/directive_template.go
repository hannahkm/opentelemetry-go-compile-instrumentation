// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"fmt"
	"strings"
	"text/template"

	"go.opentelemetry.io/otelc/tool/ex"
)

// directiveActionKeywords are the leading tokens of a "{{ ... }}" span that
// text/template treats as a real action, aside from field/variable access
// (detected separately by a leading "." or "$"). A span whose leading token
// isn't one of these is assumed to be incidental Go source rather than a
// template action - most commonly a composite literal like
// "[]Point{{X: 1, Y: 2}}", where the "{{" is just two adjacent struct
// braces - and is passed through unchanged instead of being parsed.
var directiveActionKeywords = map[string]bool{
	"if": true, "else": true, "end": true, "range": true, "with": true,
	"eq": true, "ne": true, "lt": true, "le": true, "gt": true, "ge": true,
	"and": true, "or": true, "not": true, "len": true,
}

// isDirectiveAction reports whether the content of a "{{ ... }}" span looks
// like a genuine template action rather than incidental Go source.
func isDirectiveAction(content string) bool {
	trimmed := strings.TrimSpace(strings.Trim(strings.TrimSpace(content), "-"))
	if trimmed == "" {
		return false
	}
	first := strings.Fields(trimmed)[0]
	if strings.HasPrefix(first, ".") || strings.HasPrefix(first, "$") {
		return true
	}
	return directiveActionKeywords[first]
}

// literalSentinelFormat produces placeholder text substituted for "{{ ... }}"
// spans that aren't template actions, so text/template treats them as opaque
// literal text instead of trying (and failing) to parse them as actions. The
// NUL bytes make collision with real template or Go source content
// effectively impossible.
const literalSentinelFormat = "\x00DIRECTIVE_LITERAL_%d\x00"

// escapeNonActionSpans scans text for "{{ ... }}" spans - matching the first
// "{{" to the next "}}", the same flat, non-nesting rule fasttemplate used -
// and replaces any span that isn't a recognized template action with a
// unique sentinel. It returns the sanitized text plus a
// sentinel-to-original-span map used to restore the literal spans after
// execution.
func escapeNonActionSpans(text string) (string, map[string]string, error) {
	var sb strings.Builder
	literals := make(map[string]string)
	rest := text
	n := 0
	for {
		start := strings.Index(rest, "{{")
		if start < 0 {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:start])

		afterStart := rest[start+2:]
		end := strings.Index(afterStart, "}}")
		if end < 0 {
			return "", nil, ex.Newf("cannot find end tag \"}}\" in template starting from %q", rest[start:])
		}

		content := afterStart[:end]
		span := rest[start : start+2+end+2]
		if isDirectiveAction(content) {
			sb.WriteString(span)
		} else {
			sentinel := fmt.Sprintf(literalSentinelFormat, n)
			n++
			literals[sentinel] = span
			sb.WriteString(sentinel)
		}
		rest = afterStart[end+2:]
	}
	return sb.String(), literals, nil
}

// DirectiveTemplate is a text/template-backed template for directive rule
// bodies. It wraps text/template with escaping for incidental "{{ ... }}"
// spans in the surrounding Go source that aren't meant as template actions.
type DirectiveTemplate struct {
	tmpl     *template.Template
	literals map[string]string
}

// ParseDirectiveTemplate parses a directive rule's template text.
func ParseDirectiveTemplate(text string) (*DirectiveTemplate, error) {
	sanitized, literals, err := escapeNonActionSpans(text)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("directive").Parse(sanitized)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	return &DirectiveTemplate{tmpl: tmpl, literals: literals}, nil
}

// Execute renders the template against data (typically a value exposing
// FuncName/FuncArgument/... methods) and restores any escaped literal spans.
func (d *DirectiveTemplate) Execute(data any) (string, error) {
	var sb strings.Builder
	if err := d.tmpl.Execute(&sb, data); err != nil {
		return "", ex.Wrap(err)
	}
	result := sb.String()
	for sentinel, original := range d.literals {
		result = strings.ReplaceAll(result, sentinel, original)
	}
	return result, nil
}
