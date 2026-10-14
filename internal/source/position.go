// Positions and spans inside source files.

package source

// Span is the half-open byte range [Start, End) of a token or construct.
type Span struct {
	File  *File
	Start int
	End   int
}

// Len returns the span length in bytes.
func (s Span) Len() int { return s.End - s.Start }

// Text returns the source text the span covers.
func (s Span) Text() string {
	return string(s.File.data[s.Start:s.End])
}

// StartPos returns the one-based line and rune column of the span start.
func (s Span) StartPos() (line, col int) { return s.File.LineCol(s.Start) }
