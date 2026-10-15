// Token spacing rules.

package fmt

import "zyx/internal/token"

// noSpaceBefore lists the tokens that attach to the token before them.
func noSpaceBefore(k token.Kind) bool {
	switch k {
	case token.Comma, token.RParen, token.RBrack, token.RBrace, token.Semicolon,
		token.Dot, token.Colon, token.Ellipsis:
		return true
	}
	return false
}

// noSpaceAfter lists the tokens that attach to the token after them.
func noSpaceAfter(k token.Kind) bool {
	switch k {
	case token.LParen, token.LBrack, token.Dot, token.Tilde, token.Bang:
		return true
	}
	return false
}

// opensExpression lists the tokens after which a minus sign is a sign rather
// than a subtraction.
func opensExpression(k token.Kind) bool {
	switch k {
	case token.Invalid, token.Newline, token.Comment, token.LParen, token.LBrack,
		token.LBrace, token.Comma, token.Colon, token.Semicolon, token.Assign,
		token.Plus, token.Minus, token.Star, token.Slash, token.Percent,
		token.Amp, token.Caret, token.Pipe, token.Eq, token.Ne, token.Lt, token.Le,
		token.Gt, token.Ge, token.AndAnd, token.OrOr, token.Range, token.FatArrow,
		token.Arrow, token.Return, token.In, token.If, token.Else, token.Match,
		token.For, token.Break, token.Continue, token.Defer, token.Spawn,
		token.SpawnWatched, token.Try, token.Is, token.AddAssign, token.SubAssign,
		token.MulAssign, token.DivAssign, token.ModAssign, token.AndAssign,
		token.OrAssign, token.XorAssign, token.ShlAssign, token.ShrAssign,
		token.Ellipsis:
		return true
	}
	return false
}

// opensAType lists the tokens after which `<` starts a type argument list
// rather than a comparison: a name the language capitalizes, and the
// channel keyword, which takes a type argument the same way.
func opensAType(k token.Kind, text string) bool {
	if k == token.Chan {
		return true
	}
	if k != token.Ident && k != token.SelfUpper {
		return false
	}
	return text != "" && text[0] >= 'A' && text[0] <= 'Z'
}
