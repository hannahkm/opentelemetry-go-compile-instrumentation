// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"io"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/valyala/fasttemplate"

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

// funcArgumentOfType returns the identifier of the first parameter of the
// matched function whose declared type matches typeStr (e.g.
// "context.Context").
func (d *funcTemplateData) funcArgumentOfType(typeStr string) (string, bool, error) {
	d.arguments() // ensure synthetic names are assigned to unnamed/blank params
	for _, field := range d.funcDecl.Type.Params.List {
		matched, matchErr := ast.MatchesTypeName(field.Type, typeStr)
		if matchErr != nil {
			return "", false, matchErr
		}
		if matched {
			return field.Names[0].Name, true, nil
		}
	}
	return "", false, nil
}

// numTagFields is the field count of a well-formed indexed tag, e.g.
// "FuncArgument 0" splits into ["FuncArgument", "0"].
const numTagFields = 2

// cleanTagFields trims a fasttemplate tag of surrounding whitespace and "-"
// trim markers (e.g. "{{- CallArgument 0 -}}", "{{- FuncName -}}") and splits
// what remains into whitespace-separated fields.
func cleanTagFields(tag string) []string {
	cleaned := strings.TrimSpace(tag)
	cleaned = strings.Trim(cleaned, "-")
	cleaned = strings.TrimSpace(cleaned)
	return strings.Fields(cleaned)
}

// isFuncTagVerb reports whether verb names one of the shared function
// template variables, independent of whether it can currently be resolved
// (e.g. for lack of an enclosing function).
func isFuncTagVerb(verb string) bool {
	switch verb {
	case "FuncName", "FuncArgument", "FuncReturn", "FuncArgumentCount", "FuncReturnCount":
		return true
	default:
		return false
	}
}

// resolveFuncTag attempts to resolve a fasttemplate tag as one of the shared
// function template variables (FuncName, FuncArgument N, FuncReturn N,
// FuncArgumentCount, FuncReturnCount). The tag is trimmed of surrounding
// whitespace and "-" trim markers (e.g. "{{- FuncName -}}") before matching.
//
// It returns handled=false, with no write and no error, when the tag is not
// one of these variables so callers can fall through to their own tag
// handling (e.g. call templates' "{{ . }}" placeholder).
//
// data may be nil when a tag is evaluated at a position with no enclosing
// function (e.g. a call wrapped inside a package-level variable
// initializer); in that case a recognized Func* tag still reports
// handled=true, but resolves to a descriptive error instead of a value.
func resolveFuncTag(w io.Writer, tag string, data *funcTemplateData) (int, bool, error) {
	fields := cleanTagFields(tag)
	numFields := len(fields)
	if numFields == 0 {
		return 0, false, ex.Newf("invalid template tag %q: empty tag", tag)
	}
	if !isFuncTagVerb(fields[0]) {
		return 0, false, nil
	}
	if data == nil {
		return 0, true, ex.Newf(
			"invalid template tag %q: no enclosing function is available at this position", tag,
		)
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

// renderFuncTemplate executes tmpl against the shared function template
// variables (see resolveFuncTag) and returns the resulting text. Every tag
// in tmpl must resolve via resolveFuncTag; an unresolved tag is an error.
func renderFuncTemplate(tmpl *fasttemplate.Template, data *funcTemplateData) (string, error) {
	return tmpl.ExecuteFuncStringWithErr(func(w io.Writer, tag string) (int, error) {
		n, handled, err := resolveFuncTag(w, tag, data)
		if !handled {
			return io.WriteString(w, "{{"+tag+"}}")
		}
		return n, err
	})
}
