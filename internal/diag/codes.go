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
