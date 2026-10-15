// Token values produced by the lexer and consumed by the parser.

package token

import "zyx/internal/source"

// NumSuffix is the optional type suffix on a numeric literal.
type NumSuffix uint8

// Numeric literal suffixes.
const (
	// SuffixNone means the literal takes its default type.
	SuffixNone NumSuffix = iota
	SuffixI8
	SuffixI16
	SuffixI32
	SuffixI64
	SuffixI128
	SuffixU8
	SuffixU16
	SuffixU32
	SuffixU64
	SuffixU128
	SuffixUsize
	SuffixF32
	SuffixF64
)

// suffixNames gives every suffix its source spelling.
var suffixNames = [SuffixF64 + 1]string{
	SuffixI8:    "i8",
	SuffixI16:   "i16",
	SuffixI32:   "i32",
	SuffixI64:   "i64",
	SuffixI128:  "i128",
	SuffixU8:    "u8",
	SuffixU16:   "u16",
	SuffixU32:   "u32",
	SuffixU64:   "u64",
	SuffixU128:  "u128",
	SuffixUsize: "usize",
	SuffixF32:   "f32",
	SuffixF64:   "f64",
}

// String returns the suffix spelling as written in source.
func (s NumSuffix) String() string {
	if s <= SuffixF64 {
		if name := suffixNames[s]; name != "" {
			return name
		}
	}
	return ""
}

// IsInt reports whether the suffix selects an integer type.
func (s NumSuffix) IsInt() bool { return s >= SuffixI8 && s <= SuffixUsize }

// IsFloat reports whether the suffix selects a floating-point type.
func (s NumSuffix) IsFloat() bool { return s == SuffixF32 || s == SuffixF64 }

// FStrPartKind distinguishes the two forms of an f-string segment.
type FStrPartKind uint8

// F-string segment kinds.
const (
	// FStrText is a literal text segment.
	FStrText FStrPartKind = iota
	// FStrExpr is an interpolation expression.
	FStrExpr
)

// FStrPart is one segment of an f-string literal.
type FStrPart struct {
	// Kind selects between literal text and an interpolation.
	Kind FStrPartKind
	// Text holds the decoded literal text of an FStrText segment.
	Text string
	// Span covers the source of an FStrExpr expression.
	Span source.Span
}

// Token is one lexical unit with its source range and literal payload.
type Token struct {
	// Kind is the lexical class of the token.
	Kind Kind
	// Span covers the token in the source file.
	Span source.Span
	// Str holds the decoded value of Char, Str, and ByteStr literals.
	Str string
	// Parts holds the segments of an FString literal.
	Parts []FStrPart
	// Suffix carries the type suffix of Int and Float literals.
	Suffix NumSuffix
	// Doc reports whether a Comment token is a doc comment.
	Doc bool
}

// Text returns the source text covered by the token.
func (t Token) Text() string { return t.Span.Text() }
