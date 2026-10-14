// Explicit conversion rules.

package check

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/types"
)

// checkConvert checks the conversion dst(src). The language allows only
// numeric conversions, char with u32, and raw with usize.
func (c *checker) checkConvert(dst *types.Type, src ast.Expr, sp source.Span) *types.Type {
	from := c.checkExpr(src, nil)
	if dst == nil || from == nil || from.Kind == types.Invalid {
		return dst
	}
	if !types.Convertible(dst, from) {
		c.errorHelp(diag.CodeNoConversion, sp,
			"convert between numeric types, char and u32, or raw and usize",
			"cannot convert %s to %s", from, dst)
	}
	return dst
}

// typeExprOf returns the type a conversion callee names: `i64(x)` writes an
// identifier and `net.Conn(x)` a qualified name, so the callee is an
// expression rather than a type expression.
func typeExprOf(e ast.Expr) ast.TypeExpr {
	switch v := e.(type) {
	case *ast.Ident:
		return &ast.TypeName{Sp: v.Sp, Name: v}
	case *ast.SelectorExpr:
		if q, ok := v.X.(*ast.Ident); ok && v.Name != nil {
			return &ast.TypeName{Sp: v.Sp, Qual: q, Name: v.Name}
		}
	}
	return nil
}

// isTypeSymbol reports whether a symbol names a type, which makes a call
// with that callee a conversion.
func isTypeSymbol(sym *resolve.Symbol) bool {
	if sym == nil {
		return false
	}
	switch sym.Kind {
	case resolve.KindType, resolve.KindTypeParam, resolve.KindBuiltin, resolve.KindCSymbol:
		return true
	}
	return false
}
