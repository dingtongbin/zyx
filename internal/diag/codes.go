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

// Name and visibility diagnostics occupy the E3001-E3099 range.
const (
	// CodeUndefinedName reports a reference to a name that is not declared
	// or not reachable here.
	CodeUndefinedName Code = "E3001"
	// CodeDuplicateName reports two declarations of one name in the same
	// scope, or an or-pattern whose alternatives bind different names.
	CodeDuplicateName Code = "E3002"
	// CodeShadowing reports a binding that collides with a package-level
	// declaration or a predeclared name.
	CodeShadowing Code = "E3003"
	// CodePredeclaredName reports a top-level declaration that redefines a
	// predeclared name.
	CodePredeclaredName Code = "E3004"
	// CodePrivateName reports a reference to a private name from another
	// package.
	CodePrivateName Code = "E3005"
	// CodeImportDuplicate reports two imports that share one package name
	// in the same file.
	CodeImportDuplicate Code = "E3006"
	// CodeImportUnused reports an import that no declaration of the file
	// uses.
	CodeImportUnused Code = "E3007"
	// CodeImportInvalid reports an import path that does not resolve.
	CodeImportInvalid Code = "E3008"
	// CodeExportedPrivateType reports a public declaration whose signature
	// names a private type of the same package.
	CodeExportedPrivateType Code = "E3009"
	// CodeLabelUndefined reports a break or continue target that does not
	// enclose the statement.
	CodeLabelUndefined Code = "E3010"
	// CodeLabelDuplicate reports two loops that share a label in one
	// function.
	CodeLabelDuplicate Code = "E3011"
	// CodePackageName reports a package declaration that disagrees with the
	// other files of the directory or with the import path.
	CodePackageName Code = "E3012"
	// CodeCapture reports a lambda that reads a local of an enclosing
	// function, which the language forbids.
	CodeCapture Code = "E3013"
	// CodeNotAType reports a name used as a type that does not name a type.
	CodeNotAType Code = "E3014"
	// CodeCNamespace reports use of the `c` namespace in a file that does
	// not import a C header.
	CodeCNamespace Code = "E3015"
)
