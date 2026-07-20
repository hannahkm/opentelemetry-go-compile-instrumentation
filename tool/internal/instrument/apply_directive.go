// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"io"

	"github.com/dave/dst"
	"github.com/valyala/fasttemplate"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// applyDirectiveRule finds all functions annotated with the directive, renders
// the template for each, and prepends the resulting Go statements into the
// function body.
func (ip *InstrumentPhase) applyDirectiveRule(ctx context.Context, r *rule.InstDirectiveRule, root *dst.File) error {
	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}
	tmpl, err := fasttemplate.NewTemplate(r.Template, "{{", "}}")
	if err != nil {
		return ex.Wrap(err)
	}
	funcs := ast.FindFuncsByDirective(root, r.Directive)
	for _, funcDecl := range funcs {
		var (
			snippet string
			stmts   []dst.Stmt //nolint:prealloc // Slice allocated by `p.ParseSnippet`
		)
		snippet, err = renderDirective(tmpl, newFuncTemplateData(funcDecl))
		if err != nil {
			return ex.Wrapf(err, "rendering template for func %s", funcDecl.Name.Name)
		}
		p := ast.NewAstParser()
		stmts, err = p.ParseSnippet(snippet)
		if err != nil {
			return ex.Wrapf(err, "parsing rendered template for func %s", funcDecl.Name.Name)
		}
		renameReturnValues(funcDecl)
		funcDecl.Body.List = append(stmts, funcDecl.Body.List...)
		ip.Info("Apply directive rule", "rule", r, "func", funcDecl.Name.Name)
	}
	return nil
}

// renderDirective executes the template with the given data and returns the
// resulting Go source snippet.
func renderDirective(tmpl *fasttemplate.Template, data *funcTemplateData) (string, error) {
	return tmpl.ExecuteFuncStringWithErr(func(w io.Writer, tag string) (int, error) {
		n, handled, err := resolveFuncTag(w, tag, data)
		if !handled {
			return 0, ex.Newf("unknown template tag %q", tag)
		}
		return n, err
	})
}
