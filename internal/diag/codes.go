// Stable diagnostic codes, append-only across compiler releases.

package diag

// Code is a stable diagnostic identifier such as "E1001". A code never
// changes meaning once assigned.
type Code string

// Lexical diagnostics occupy the E1001-E1099 range.
const (
	// CodeUnexpectedChar reports a byte that starts no valid token.
	CodeUnexpectedChar Code = "E1001"
	// CodeDoubleColon reports the forbidden token "::".
	CodeDoubleColon Code = "E1002"
	// CodeInvalidEscape reports an unknown or malformed escape sequence.
	CodeInvalidEscape Code = "E1003"
	// CodeUnterminatedLiteral reports a literal left open at end of line
	// or end of file.
	CodeUnterminatedLiteral Code = "E1004"
	// CodeInvalidNumber reports a malformed numeric literal.
	CodeInvalidNumber Code = "E1005"
	// CodeNonASCIISource reports non-ASCII outside a comment or literal.
	CodeNonASCIISource Code = "E1006"
	// CodeInvalidCharLiteral reports a char literal that is not exactly
	// one character.
	CodeInvalidCharLiteral Code = "E1007"
	// CodeInvalidUTF8 reports source bytes that are not valid UTF-8.
	CodeInvalidUTF8 Code = "E1008"
	// CodeNestedFString reports an f-string inside an interpolation.
	CodeNestedFString Code = "E1009"
	// CodeEmptyInterpolation reports an interpolation with no expression.
	CodeEmptyInterpolation Code = "E1010"
)

// Syntax diagnostics occupy the E2001-E2099 range.
const (
	// CodeExpectedToken reports a token other than the one the grammar
	// requires.
	CodeExpectedToken Code = "E2001"
	// CodeUnexpectedToken reports a token that starts no construct in its
	// position.
	CodeUnexpectedToken Code = "E2002"
	// CodeInvalidStatement reports an expression that cannot stand alone as
	// a statement.
	CodeInvalidStatement Code = "E2003"
	// CodeInvalidAssignTarget reports an assignment to something other than
	// a variable, field, or index.
	CodeInvalidAssignTarget Code = "E2004"
	// CodeMisplacedDecl reports a declaration in a position the file
	// structure forbids.
	CodeMisplacedDecl Code = "E2005"
	// CodeDocComment reports a doc comment that attaches to no declaration.
	CodeDocComment Code = "E2006"
	// CodeChainedOperator reports a non-chainable operator used twice.
	CodeChainedOperator Code = "E2007"
	// CodeFloatPattern reports a float literal used as a match pattern.
	CodeFloatPattern Code = "E2008"
	// CodeInvalidTupleIndex reports a tuple element position that is not a
	// plain integer.
	CodeInvalidTupleIndex Code = "E2009"
	// CodeUnitTuple reports a parenthesized one-element tuple.
	CodeUnitTuple Code = "E2010"
	// CodeDeclarationInSingleLineBody reports a declaration inside a
	// single-line body.
	CodeDeclarationInSingleLineBody Code = "E2011"
)
