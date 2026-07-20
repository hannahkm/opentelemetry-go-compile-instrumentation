// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"io"
	"strconv"
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
)

// funcTemplateData lazily exposes a matched function's name, arguments, and
// return values to template renderers. Argument and return collection mutates
// the underlying FuncDecl.
type funcTemplateData struct {
	funcDecl *dst.FuncDecl

	argsCollected bool
	args          []string

	retsCollected bool
	rets          []string
}

func newFuncTemplateData(funcDecl *dst.FuncDecl) *funcTemplateData {
	return &funcTemplateData{funcDecl: funcDecl}
}

func (d *funcTemplateData) name() string {
	return d.funcDecl.Name.Name
}

// arguments returns the matched function's parameter identifiers, excluding
// the receiver.
func (d *funcTemplateData) arguments() []string {
	if !d.argsCollected {
		args := collectArguments(d.funcDecl)
		if ast.HasReceiver(d.funcDecl) {
			args = args[1:]
		}
		d.args = args
		d.argsCollected = true
	}
	return d.args
}

func (d *funcTemplateData) returns() []string {
	if !d.retsCollected {
		d.rets = collectReturnValues(d.funcDecl)
		d.retsCollected = true
	}
	return d.rets
}

// numTagFields is the field count of a well-formed indexed tag, e.g.
// "FuncArgument 0" splits into ["FuncArgument", "0"].
const numTagFields = 2

// resolveFuncTag attempts to resolve a fasttemplate tag as one of the shared
// function template variables (FuncName, FuncArgument N, FuncReturn N,
// FuncArgumentCount, FuncReturnCount). The tag is trimmed of surrounding
// whitespace and "-" trim markers (e.g. "{{- FuncName -}}") before matching.
//
// It returns handled=false, with no write and no error, when the tag is not
// one of these variables so callers can fall through to their own tag
// handling (e.g. call templates' "{{ . }}" placeholder).
func resolveFuncTag(w io.Writer, tag string, data *funcTemplateData) (int, bool, error) {
	cleaned := strings.TrimSpace(tag)
	cleaned = strings.Trim(cleaned, "-")
	cleaned = strings.TrimSpace(cleaned)

	fields := strings.Fields(cleaned)
	numFields := len(fields)
	if numFields == 0 {
		return 0, false, nil
	}

	switch fields[0] {
	case "FuncName":
		if numFields != 1 {
			return 0, true, ex.Newf("invalid template tag %q: FuncName takes no argument", tag)
		}
		n, err := io.WriteString(w, data.name())
		return n, true, err
	case "FuncArgument":
		return resolveIndexedTag(w, tag, fields, data.arguments(), "FuncArgument")
	case "FuncReturn":
		return resolveIndexedTag(w, tag, fields, data.returns(), "FuncReturn")
	case "FuncArgumentCount":
		if numFields != 1 {
			return 0, true, ex.Newf("invalid template tag %q: FuncArgumentCount takes no argument", tag)
		}
		n, err := io.WriteString(w, strconv.Itoa(len(data.arguments())))
		return n, true, err
	case "FuncReturnCount":
		if numFields != 1 {
			return 0, true, ex.Newf("invalid template tag %q: FuncReturnCount takes no argument", tag)
		}
		n, err := io.WriteString(w, strconv.Itoa(len(data.returns())))
		return n, true, err
	default:
		return 0, false, nil
	}
}

func resolveIndexedTag(w io.Writer, tag string, fields, values []string, verb string) (int, bool, error) {
	if len(fields) != numTagFields {
		return 0, true, ex.Newf("invalid template tag %q: %s requires exactly one index argument", tag, verb)
	}
	idx, convErr := strconv.Atoi(fields[1])
	if convErr != nil {
		return 0, true, ex.Newf("invalid template tag %q: %s index %q is not an integer", tag, verb, fields[1])
	}
	if idx < 0 || idx >= len(values) {
		return 0, true, ex.Newf(
			"invalid template tag %q: %s index %d out of range [0, %d)", tag, verb, idx, len(values),
		)
	}
	n, err := io.WriteString(w, values[idx])
	return n, true, err
}
