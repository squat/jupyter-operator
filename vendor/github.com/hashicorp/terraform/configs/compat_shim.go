package configs

import (
	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hcl/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// -------------------------------------------------------------------------
// Functions in this file are compatibility shims intended to ease conversion
// from the old configuration loader. Any use of these functions that makes
// a change should generate a deprecation warning explaining to the user how
// to update their code for new patterns.
//
// Shims are particularly important for any patterns that have been widely
// documented in books, tutorials, etc. Users will still be starting from
// these examples and we want to help them adopt the latest patterns rather
// than leave them stranded.
// -------------------------------------------------------------------------

// shimTraversalInString takes any arbitrary expression and checks if it is
// a quoted string in the native syntax. If it _is_, then it is parsed as a
// traversal and re-wrapped into a synthetic traversal expression and a
// warning is generated. Otherwise, the given expression is just returned
// verbatim.
//
// This function has no effect on expressions from the JSON syntax, since
// traversals in strings are the required pattern in that syntax.
//
// If wantKeyword is set, the generated warning diagnostic will talk about
// keywords rather than references. The behavior is otherwise unchanged, and
// the caller remains responsible for checking that the result is indeed
// a keyword, e.g. using hcl.ExprAsKeyword.
func shimTraversalInString(expr hcl.Expression, wantKeyword bool) (hcl.Expression, hcl.Diagnostics) {
	if !exprIsNativeQuotedString(expr) {
		return expr, nil
	}

	strVal, diags := expr.Value(nil)
	if diags.HasErrors() || strVal.IsNull() || !strVal.IsKnown() {
		// Since we're not even able to attempt a shim here, we'll discard
		// the diagnostics we saw so far and let the caller's own error
		// handling take care of reporting the invalid expression.
		return expr, nil
	}

	// The position handling here isn't _quite_ right because it won't
	// take into account any escape sequences in the literal string, but
	// it should be close enough for any error reporting to make sense.
	srcRange := expr.Range()
	startPos := srcRange.Start // copy
	startPos.Column++          // skip initial quote
	startPos.Byte++            // skip initial quote

	traversal, tDiags := hclsyntax.ParseTraversalAbs(
		[]byte(strVal.AsString()),
		srcRange.Filename,
		startPos,
	)
	diags = append(diags, tDiags...)

	// For initial release our deprecation warnings are disabled to allow
	// a period where modules can be compatible with both old and new
	// conventions.
	// FIXME: Re-enable these deprecation warnings in a release prior to
	// Terraform 0.13 and then remove the shims altogether for 0.13.
	/*
		if wantKeyword {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Quoted keywords are deprecated",
				Detail:   "In this context, keywords are expected literally rather than in quotes. Previous versions of Terraform required quotes, but that usage is now deprecated. Remove the quotes surrounding this keyword to silence this warning.",
				Subject:  &srcRange,
			})
		} else {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Quoted references are deprecated",
				Detail:   "In this context, references are expected literally rather than in quotes. Previous versions of Terraform required quotes, but that usage is now deprecated. Remove the quotes surrounding this reference to silence this warning.",
				Subject:  &srcRange,
			})
		}
	*/

	return &hclsyntax.ScopeTraversalExpr{
		Traversal: traversal,
		SrcRange:  srcRange,
	}, diags
}

// shimIsIgnoreChangesStar returns true if the given expression seems to be
// a string literal whose value is "*". This is used to support a legacy
// form of ignore_changes = all .
//
// This function does not itself emit any diagnostics, so it's the caller's
// responsibility to emit a warning diagnostic when this function returns true.
func shimIsIgnoreChangesStar(expr hcl.Expression) bool {
	val, valDiags := expr.Value(nil)
	if valDiags.HasErrors() {
		return false
	}
	if val.Type() != cty.String || val.IsNull() || !val.IsKnown() {
		return false
	}
	return val.AsString() == "*"
}
