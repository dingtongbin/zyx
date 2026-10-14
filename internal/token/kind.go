// Package token defines token kinds and token values.
package token

import "strconv"

// Kind identifies the lexical class of a token.
type Kind uint8

// Token kinds. The zero value is Invalid.
const (
	// Invalid marks a byte sequence the lexer could not accept.
	Invalid Kind = iota
	// EOF marks the end of the token stream.
	EOF
	// Newline is a statement terminator: a line break or a semicolon.
	Newline
	// Comment is a line comment; Token.Doc distinguishes doc comments.
	Comment
	// Ident is a non-keyword identifier.
	Ident
	// Underscore is the standalone discard binding.
	Underscore
	// Int is an integer literal; Token.Suffix carries its type suffix.
	Int
	// Float is a floating-point literal; Token.Suffix carries its type suffix.
	Float
	// Char is a character literal; Token.Str holds the decoded character.
	Char
	// Str is a string literal; Token.Str holds the decoded text.
	Str
	// FString is an interpolated string literal made of Token.Parts.
	FString
	// ByteStr is a byte string literal; Token.Str holds the raw bytes.
	ByteStr

	// The language keywords, in source spelling.
	Package
	Import
	Const
	Var
	Type
	Func
	Struct
	Enum
	Interface
	Impl
	SelfLower
	SelfUpper
	New
	If
	Else
	Match
	For
	In
	Break
	Continue
	Return
	Defer
	Region
	Is
	Try
	Comptime
	Spawn
	SpawnWatched
	Chan
	Select
	Recv
	Send
	Timeout
	Default
	Extern
	C
	Fast
	True
	False

	// Operators and punctuation.
	Plus
	Minus
	Star
	Slash
	Percent
	Shl
	Shr
	Amp
	Caret
	Pipe
	Tilde
	Bang
	Eq
	Ne
	Lt
	Le
	Gt
	Ge
	AndAnd
	OrOr
	Range
	FatArrow
	Arrow
	Ellipsis
	Comma
	Dot
	Colon
	Assign
	AddAssign
	SubAssign
	MulAssign
	DivAssign
	ModAssign
	AndAssign
	OrAssign
	XorAssign
	ShlAssign
	ShrAssign
	LParen
	RParen
	LBrack
	RBrack
	LBrace
	RBrace

	// NumKinds is the number of token kinds.
	NumKinds
)

// kindNames gives every kind a stable debug and test name.
var kindNames = [NumKinds]string{
	Invalid:      "invalid",
	EOF:          "EOF",
	Newline:      "newline",
	Comment:      "comment",
	Ident:        "ident",
	Underscore:   "_",
	Int:          "int",
	Float:        "float",
	Char:         "char",
	Str:          "str",
	FString:      "fstring",
	ByteStr:      "bytestring",
	Package:      "package",
	Import:       "import",
	Const:        "const",
	Var:          "var",
	Type:         "type",
	Func:         "func",
	Struct:       "struct",
	Enum:         "enum",
	Interface:    "interface",
	Impl:         "impl",
	SelfLower:    "self",
	SelfUpper:    "Self",
	New:          "new",
	If:           "if",
	Else:         "else",
	Match:        "match",
	For:          "for",
	In:           "in",
	Break:        "break",
	Continue:     "continue",
	Return:       "return",
	Defer:        "defer",
	Region:       "region",
	Is:           "is",
	Try:          "try",
	Comptime:     "comptime",
	Spawn:        "spawn",
	SpawnWatched: "spawn_watched",
	Chan:         "chan",
	Select:       "select",
	Recv:         "recv",
	Send:         "send",
	Timeout:      "timeout",
	Default:      "default",
	Extern:       "extern",
	C:            "c",
	Fast:         "fast",
	True:         "true",
	False:        "false",
	Plus:         "+",
	Minus:        "-",
	Star:         "*",
	Slash:        "/",
	Percent:      "%",
	Shl:          "<<",
	Shr:          ">>",
	Amp:          "&",
	Caret:        "^",
	Pipe:         "|",
	Tilde:        "~",
	Bang:         "!",
	Eq:           "==",
	Ne:           "!=",
	Lt:           "<",
	Le:           "<=",
	Gt:           ">",
	Ge:           ">=",
	AndAnd:       "&&",
	OrOr:         "||",
	Range:        "..",
	FatArrow:     "=>",
	Arrow:        "->",
	Ellipsis:     "...",
	Comma:        ",",
	Dot:          ".",
	Colon:        ":",
	Assign:       "=",
	AddAssign:    "+=",
	SubAssign:    "-=",
	MulAssign:    "*=",
	DivAssign:    "/=",
	ModAssign:    "%=",
	AndAssign:    "&=",
	OrAssign:     "|=",
	XorAssign:    "^=",
	ShlAssign:    "<<=",
	ShrAssign:    ">>=",
	LParen:       "(",
	RParen:       ")",
	LBrack:       "[",
	RBrack:       "]",
	LBrace:       "{",
	RBrace:       "}",
}

// String returns the source spelling of a keyword or operator, or a debug
// name for the lexical classes that never appear verbatim.
func (k Kind) String() string {
	if k < NumKinds {
		if s := kindNames[k]; s != "" {
			return s
		}
	}
	return "kind(" + strconv.Itoa(int(k)) + ")"
}

// keywords maps the 39 language keywords to their kinds.
var keywords = map[string]Kind{
	"package":       Package,
	"import":        Import,
	"const":         Const,
	"var":           Var,
	"type":          Type,
	"func":          Func,
	"struct":        Struct,
	"enum":          Enum,
	"interface":     Interface,
	"impl":          Impl,
	"self":          SelfLower,
	"Self":          SelfUpper,
	"new":           New,
	"if":            If,
	"else":          Else,
	"match":         Match,
	"for":           For,
	"in":            In,
	"break":         Break,
	"continue":      Continue,
	"return":        Return,
	"defer":         Defer,
	"region":        Region,
	"is":            Is,
	"try":           Try,
	"comptime":      Comptime,
	"spawn":         Spawn,
	"spawn_watched": SpawnWatched,
	"chan":          Chan,
	"select":        Select,
	"recv":          Recv,
	"send":          Send,
	"timeout":       Timeout,
	"default":       Default,
	"extern":        Extern,
	"c":             C,
	"fast":          Fast,
	"true":          True,
	"false":         False,
}

// LookupKeyword reports the kind of text when text is a keyword.
func LookupKeyword(text string) (Kind, bool) {
	k, ok := keywords[text]
	return k, ok
}
