// Command strcase generates the Unicode simple case-mapping ranges that the
// std/str package holds. It reads the mappings from the Go toolchain's own
// Unicode tables, so the ranges are derived from the Unicode data rather than
// transcribed, and writes std/str/case.zyx. Run it from the repository root:
//
//	go run ./std/str/gen
//
// One code point maps to at most one code point, which is what a mainstream
// language's simple case mapping does; a mapping to several code points is not
// part of this table.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unicode"
)

// caseRange is one run of consecutive code points that share a mapping delta.
type caseRange struct {
	lo    rune
	hi    rune
	delta int
}

// fold walks every code point and collects the longest runs of consecutive
// code points whose mapping differs from the point itself by one delta. The
// runs are sorted by their first code point, which is the order the package's
// binary search expects.
func fold(mapped func(rune) rune) []caseRange {
	var out []caseRange
	for cp := rune(0); cp <= unicode.MaxRune; cp++ {
		to := mapped(cp)
		if to == cp {
			continue
		}
		delta := int(to) - int(cp)
		if n := len(out); n > 0 && out[n-1].hi == cp-1 && out[n-1].delta == delta {
			out[n-1].hi = cp
			continue
		}
		out = append(out, caseRange{lo: cp, hi: cp, delta: delta})
	}
	return out
}

// output is the file this command writes, relative to the repository root.
const output = "std/str/case.zyx"

func main() {
	upper := fold(unicode.ToUpper)
	lower := fold(unicode.ToLower)
	var b []byte
	b = append(b, header...)
	b = appendTable(b, "upper", "upper", upper)
	b = appendTable(b, "lower", "lower", lower)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(output, b, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s: %d upper ranges, %d lower ranges (Unicode %s)\n", output, len(upper), len(lower), unicode.Version)
}

const header = `package str

// The Unicode simple case mappings, one code point to at most one code point.
// Each table holds the runs of consecutive code points that differ from their
// mapping, in order; a code point inside a run maps by adding the run's delta.
// The runs are generated from the Unicode data by gen/main.go, not written by
// hand, and the count beside each table is its length. The tables are private,
// because the type of a run is.

// case_range is one run of code points that share a case-mapping delta.
extern struct case_range {
    lo: u32
    hi: u32
    delta: i32
}

`

// appendTable writes one table and the constant that names its length.
func appendTable(b []byte, name, title string, rs []caseRange) []byte {
	b = fmt.Appendf(b, "// %s is the table of %s mappings that differ from the code point.\n", name, title)
	b = fmt.Appendf(b, "const %s: [case_range; %d] = [\n", name, len(rs))
	for _, r := range rs {
		b = fmt.Appendf(b, "    case_range { lo: 0x%X, hi: 0x%X, delta: %d },\n", r.lo, r.hi, r.delta)
	}
	b = fmt.Appendf(b, "]\n\n// %s_ranges is how many runs %s holds.\nconst %s_ranges: usize = %d\n\n", title, name, title, len(rs))
	return b
}
