// Pattern matching: bindings, variant resolution, and exhaustiveness.

package check

import (
	"strings"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/types"
)

// checkMatch checks a match statement: the subject, then each arm with its
// pattern bindings and guard, and finally whether the arms cover every
// value.
func (c *checker) checkMatch(v *ast.MatchStmt) {
	subject := c.checkExpr(v.Subject, nil)
	for _, arm := range v.Arms {
		c.checkPattern(arm.Pattern, subject)
		if arm.Guard != nil {
			c.checkCond(arm.Guard)
		}
		c.checkStmt(arm.Body)
	}
	c.checkExhaustive(v, subject)
}

// checkPattern checks one pattern against the type of the value it matches.
func (c *checker) checkPattern(p ast.Pattern, subject *types.Type) {
	switch v := p.(type) {
	case nil:
	case *ast.LitPattern:
		t := c.literalType(v.X, subject)
		c.out.Nodes[v.X] = t
		if subject != nil && subject.Kind != types.Invalid && t != nil &&
			t.Kind != types.Invalid && !types.Identical(t, subject) {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"match a literal of the subject's type",
				"pattern %s cannot match %s", t, subject)
		}
	case *ast.WildcardPattern:
		// A wildcard matches anything.
	case *ast.BindPattern:
		c.checkBindPattern(v, subject)
	case *ast.VariantPattern:
		c.checkVariantPattern(v, subject)
	case *ast.TuplePattern:
		c.checkTuplePattern(v, subject)
	case *ast.OrPattern:
		for _, alt := range v.Patterns {
			c.checkPattern(alt, subject)
		}
	case *ast.RangePattern:
		c.checkRangePattern(v, subject)
	}
}

// checkBindPattern checks a bare name. It is a variant when the subject is
// an enum with a variant of that name, and a binding otherwise.
func (c *checker) checkBindPattern(v *ast.BindPattern, subject *types.Type) {
	if v.Name == nil {
		return
	}
	if d := c.declOf(subject); d != nil && subject.Kind == types.KindEnum {
		if variant := d.Variant(v.Name.Name); variant != nil {
			if len(variant.Payload) > 0 {
				c.errorHelp(diag.CodeTypeMismatch, v.Name.Sp,
					"write the payload pattern after the variant name",
					"%s.%s takes a payload", subject.Name, v.Name.Name)
			}
			return
		}
	}
	if resolve.Public(v.Name.Name) {
		c.errorHelp(diag.CodeUppercaseLocal, v.Name.Sp,
			"write the binding in snake_case",
			"local name %q starts with an uppercase letter", v.Name.Name)
	}
	c.bindName(v.Name, subject)
}

// checkVariantPattern checks `Ok(v)`, which matches one variant of the
// subject's enum.
func (c *checker) checkVariantPattern(v *ast.VariantPattern, subject *types.Type) {
	if v.Name == nil {
		return
	}
	d := c.declOf(subject)
	if subject == nil || subject.Kind != types.KindEnum || d == nil {
		c.errorHelp(diag.CodeTypeMismatch, v.Name.Sp,
			"match an enum, or bind the value",
			"%s is not an enum", subject)
		c.checkPatterns(v.Payload, nil)
		return
	}
	variant := d.Variant(v.Name.Name)
	if variant == nil {
		c.errorHelp(diag.CodeNoField, v.Name.Sp,
			"check the variant name",
			"%s has no variant %q", subject.Name, v.Name.Name)
		c.checkPatterns(v.Payload, nil)
		return
	}
	if len(v.Payload) != len(variant.Payload) {
		c.errorHelp(diag.CodeArgCount, v.Name.Sp,
			"write one pattern per payload value",
			"%s.%s has %d payload values, got %d patterns",
			subject.Name, v.Name.Name, len(variant.Payload), len(v.Payload))
	}
	for i, p := range v.Payload {
		var want *types.Type
		if i < len(variant.Payload) {
			want = c.substituteFor(subject, d, variant.Payload[i])
		}
		c.checkPattern(p, want)
	}
}

// checkPatterns checks a pattern list against no subject type.
func (c *checker) checkPatterns(patterns []ast.Pattern, subject *types.Type) {
	for _, p := range patterns {
		c.checkPattern(p, subject)
	}
}

// checkTuplePattern checks `(p, q)` against a tuple subject.
func (c *checker) checkTuplePattern(v *ast.TuplePattern, subject *types.Type) {
	if subject != nil && subject.Kind != types.Invalid && subject.Kind != types.KindTuple {
		c.errorHelp(diag.CodeTypeMismatch, v.Sp,
			"match a tuple with a tuple pattern",
			"%s is not a tuple", subject)
		c.checkPatterns(v.Elems, nil)
		return
	}
	if subject != nil && len(subject.Elems) != len(v.Elems) {
		c.errorHelp(diag.CodeArgCount, v.Sp,
			"write one pattern per tuple element",
			"%s has %d elements, got %d patterns", subject, len(subject.Elems), len(v.Elems))
	}
	for i, p := range v.Elems {
		var want *types.Type
		if subject != nil && i < len(subject.Elems) {
			want = subject.Elems[i]
		}
		c.checkPattern(p, want)
	}
}

// checkRangePattern checks `0..10` against an ordered subject.
func (c *checker) checkRangePattern(v *ast.RangePattern, subject *types.Type) {
	for _, e := range []ast.Expr{v.Lo, v.Hi} {
		t := c.checkExpr(e, subject)
		if subject != nil && subject.Kind != types.Invalid && t != nil &&
			t.Kind != types.Invalid && !types.Identical(t, subject) {
			c.errorHelp(diag.CodeTypeMismatch, spanOf(e),
				"write a bound of the subject's type",
				"bound %s cannot match %s", t, subject)
		}
	}
	if subject != nil && subject.Kind != types.Invalid && !subject.IsOrdered() {
		c.errorHelp(diag.CodeTypeMismatch, v.Sp,
			"use a range over numbers or char",
			"%s is not ordered", subject)
	}
}

// checkExhaustive reports a match that leaves values uncovered.
func (c *checker) checkExhaustive(v *ast.MatchStmt, subject *types.Type) {
	if c.matchExhaustive(v) {
		return
	}
	if subject != nil && subject.Kind == types.KindEnum {
		c.errorHelp(diag.CodeMissingArm, v.Sp,
			"cover every variant, or end the match with a wildcard arm",
			"%s does not cover %s", subject.Name, c.missingVariants(v, subject))
		return
	}
	c.errorHelp(diag.CodeMissingArm, v.Sp,
		"end the match with a wildcard arm",
		"a match over %s needs a wildcard arm", subject)
}

// missingVariants lists the variants a match does not cover.
func (c *checker) missingVariants(v *ast.MatchStmt, subject *types.Type) string {
	d := c.declOf(subject)
	if d == nil {
		return "every variant"
	}
	covered := map[string]bool{}
	for _, arm := range v.Arms {
		collectCovered(arm.Pattern, subject, c, covered)
	}
	var missing []string
	for _, variant := range d.Variants {
		if !covered[variant.Name] {
			missing = append(missing, variant.Name)
		}
	}
	return strings.Join(missing, ", ")
}

// matchExhaustive reports whether every value is covered: an arm that binds
// anything, or every variant of the subject's enum.
func (c *checker) matchExhaustive(v *ast.MatchStmt) bool {
	subject := c.out.Nodes[v.Subject]
	covered := map[string]bool{}
	for _, arm := range v.Arms {
		if arm.Guard != nil {
			continue // a guard can fail
		}
		if collectCovered(arm.Pattern, subject, c, covered) {
			return true
		}
	}
	if subject == nil || subject.Kind != types.KindEnum {
		return false
	}
	d := c.declOf(subject)
	if d == nil {
		return false
	}
	for _, variant := range d.Variants {
		if !covered[variant.Name] {
			return false
		}
	}
	return true
}

// collectCovered records the variants a pattern covers and reports whether
// the pattern is irrefutable, so no later value can reach a further arm.
func collectCovered(p ast.Pattern, subject *types.Type, c *checker, covered map[string]bool) bool {
	switch v := p.(type) {
	case *ast.WildcardPattern:
		return true
	case *ast.BindPattern:
		if v.Name == nil {
			return false
		}
		if d := c.declOf(subject); d != nil && subject.Kind == types.KindEnum &&
			d.Variant(v.Name.Name) != nil {
			covered[v.Name.Name] = true
			return false
		}
		return true
	case *ast.VariantPattern:
		if v.Name != nil {
			covered[v.Name.Name] = true
		}
		return false
	case *ast.OrPattern:
		for _, alt := range v.Patterns {
			if collectCovered(alt, subject, c, covered) {
				return true
			}
		}
		return false
	}
	return false
}
