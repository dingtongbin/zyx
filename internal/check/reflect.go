// The meta and dur namespaces: reflection, layout queries, and the
// compile-time duration constructors.

package check

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/token"
	"zyx/internal/types"
)

// metaCall gives the shape of one meta query: how many of its arguments are
// written as types rather than values, and its result.
type metaCall struct {
	// typeArgs is the number of leading arguments written as types.
	typeArgs int
	// valueArgs is the number of arguments written as values.
	valueArgs int
	// result is the query's result type; nil when the result is the
	// declared type of the type body constant the query names.
	result *types.Type
}

// metaQueries lists every query of the meta namespace.
var metaQueries = map[string]metaCall{
	"typeof":      {valueArgs: 1, result: types.TypeValue},
	"name":        {typeArgs: 1, result: types.Str},
	"target":      {valueArgs: 0, result: types.Str},
	"field_count": {typeArgs: 1, result: types.Usize},
	"field_name":  {typeArgs: 1, valueArgs: 1, result: types.Str},
	"field_type":  {typeArgs: 1, valueArgs: 1, result: types.TypeValue},
	"offset_of":   {typeArgs: 1, valueArgs: 1, result: types.Usize},
	"size_of":     {typeArgs: 1, result: types.Usize},
	"align_of":    {typeArgs: 1, result: types.Usize},
	"enum_count":  {typeArgs: 1, result: types.Usize},
	"enum_name":   {valueArgs: 1, result: types.Str},
	"has_field":   {typeArgs: 1, valueArgs: 1, result: types.Bool},
	"implements":  {typeArgs: 2, result: types.Bool},
	"attr":        {typeArgs: 1, valueArgs: 1},
}

// namespaceValue reports a bare name of the meta or dur namespace, which is
// only usable in a call.
func (c *checker) namespaceValue(v *ast.SelectorExpr, ns string) *types.Type {
	if ns == "c" {
		// The c namespace names what a C header declares, which a value
		// position reads as a constant, a global object, or a type.
		return c.cValue(v)
	}
	c.unknownNamespaceName(v, ns)
	c.errorHelp(diag.CodeMetaName, v.Sp,
		"call the query",
		"%s.%s is only usable in a call", ns, v.Name.Name)
	return nil
}

// unknownNamespaceName reports a name neither namespace declares.
func (c *checker) unknownNamespaceName(v *ast.SelectorExpr, ns string) {
	if v.Name == nil {
		return
	}
	if ns == "dur" {
		switch v.Name.Name {
		case "ms", "us", "sec":
			return
		}
		c.errorHelp(diag.CodeMetaName, v.Name.Sp,
			"use dur.ms, dur.us, or dur.sec",
			"dur has no constructor %q", v.Name.Name)
		return
	}
	if _, ok := metaQueries[v.Name.Name]; !ok {
		c.errorHelp(diag.CodeMetaName, v.Name.Sp,
			"check the query name",
			"meta has no query %q", v.Name.Name)
	}
}

// checkNamespaceCall checks a call into the meta or dur namespace, which is
// only available at compile time. A query about a type parameter is the
// exception: the answer is settled when the declaration that asks is
// instantiated, so the call is left where it stands.
func (c *checker) checkNamespaceCall(sel *ast.SelectorExpr, v *ast.CallExpr, ns string) *types.Type {
	if !c.inComptime && !c.mentionsTypeParam(v) {
		c.errorHelp(diag.CodeComptimeOnly, sel.Sp,
			"move the call into a comptime context, such as a comptime func or a constant initializer",
			"%s.%s is only available at compile time", ns, sel.Name.Name)
	}
	c.unknownNamespaceName(sel, ns)
	if ns == "dur" {
		return c.checkDurCall(sel, v)
	}
	return c.checkMetaCall(sel, v)
}

// mentionsTypeParam reports whether an argument names a type parameter, which
// is what a layout query inside a generic body asks about.
func (c *checker) mentionsTypeParam(v *ast.CallExpr) bool {
	for _, a := range v.Args {
		id, ok := a.(*ast.Ident)
		if !ok {
			continue
		}
		if sym := c.symOf(id); sym != nil && sym.Kind == resolve.KindTypeParam {
			return true
		}
	}
	return false
}

// checkDurCall checks the compile-time duration constructors.
func (c *checker) checkDurCall(sel *ast.SelectorExpr, v *ast.CallExpr) *types.Type {
	switch sel.Name.Name {
	case "ms", "us", "sec":
		if len(v.Args) != 1 {
			c.errorHelp(diag.CodeArgCount, sel.Sp,
				"pass one argument",
				"dur.%s takes one argument", sel.Name.Name)
		}
		for _, a := range v.Args {
			c.checkExpr(a, types.Uint64)
		}
		return types.Duration
	}
	c.checkExprs(v.Args)
	return nil
}

// checkMetaCall checks one query of the meta namespace.
func (c *checker) checkMetaCall(sel *ast.SelectorExpr, v *ast.CallExpr) *types.Type {
	q, ok := metaQueries[sel.Name.Name]
	if !ok {
		c.checkExprs(v.Args)
		return nil
	}
	want := q.typeArgs + q.valueArgs
	if len(v.Args) != want {
		c.errorHelp(diag.CodeArgCount, sel.Sp,
			"pass the declared number of arguments",
			"meta.%s takes %d arguments, got %d", sel.Name.Name, want, len(v.Args))
	}
	for i, a := range v.Args {
		if i < q.typeArgs {
			c.buildType(typeExprOf(a))
			continue
		}
		c.checkExpr(a, nil)
	}
	if q.result != nil {
		return q.result
	}
	// meta.attr returns the declared type of the constant it names.
	return c.attrType(v)
}

// attrType returns the declared type of the type body constant that
// meta.attr names.
func (c *checker) attrType(v *ast.CallExpr) *types.Type {
	if len(v.Args) < 2 {
		return nil
	}
	lit, ok := v.Args[1].(*ast.Literal)
	if !ok || lit.Kind != token.Str {
		return nil
	}
	d := c.declOf(c.buildType(typeExprOf(v.Args[0])))
	if d == nil {
		return nil
	}
	con, ok := d.Consts[lit.Value]
	if !ok {
		return nil
	}
	return con.Type
}
