// Package source models source files and their line maps.
package source

import (
	"sort"
	"unicode/utf8"
)

// utf8BOM is the byte order mark stripped from the head of every file so
// that offsets, line maps, and diagnostics agree on a single base.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// File is one source file held in memory. Positions are byte offsets into
// Data, and columns count runes so diagnostics stay correct in literals.
type File struct {
	name  string
	data  []byte
	lines []int // byte offset of each line start; lines[0] is always 0
}

// NewFile builds the line map for data. A leading UTF-8 BOM is dropped, and
// the name appears verbatim in diagnostics.
func NewFile(name string, data []byte) *File {
	if len(data) >= len(utf8BOM) && string(data[:len(utf8BOM)]) == string(utf8BOM) {
		data = data[len(utf8BOM):]
	}
	f := &File{name: name, data: data, lines: []int{0}}
	for i, b := range data {
		if b == '\n' {
			f.lines = append(f.lines, i+1)
		}
	}
	return f
}

// Name returns the file name used in diagnostics.
func (f *File) Name() string { return f.name }

// Data returns the raw file contents.
func (f *File) Data() []byte { return f.data }

// Len returns the file size in bytes.
func (f *File) Len() int { return len(f.data) }

// LineCount returns the number of lines. Every file has at least one line.
func (f *File) LineCount() int { return len(f.lines) }

// LineCol maps a byte offset to a one-based line and rune column.
// The offset must be in [0, Len].
func (f *File) LineCol(offset int) (line, col int) {
	if offset < 0 || offset > len(f.data) {
		panic("source: offset out of range")
	}
	idx := sort.Search(len(f.lines), func(i int) bool {
		return f.lines[i] > offset
	}) - 1
	col = 1
	for i := f.lines[idx]; i < offset; {
		_, size := utf8.DecodeRune(f.data[i:])
		i += size
		col++
	}
	return idx + 1, col
}
