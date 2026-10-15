package token

import "testing"

// keywordSpellings lists the 40 keywords of the language in source spelling.
var keywordSpellings = []string{
	"package", "import", "const", "var", "type", "func", "struct", "union",
	"enum", "interface", "impl", "self", "Self", "new", "if", "else", "match",
	"for", "in", "break", "continue", "return", "defer", "region", "is",
	"try", "comptime", "spawn", "spawn_watched", "chan", "select", "recv",
	"send", "timeout", "default", "extern", "c", "fast", "true", "false",
}

func TestKeywordTable(t *testing.T) {
	if got := len(keywords); got != len(keywordSpellings) {
		t.Fatalf("keyword table has %d entries, want %d", got, len(keywordSpellings))
	}
	for _, spelling := range keywordSpellings {
		k, ok := LookupKeyword(spelling)
		if !ok {
			t.Errorf("LookupKeyword(%q) reported no keyword", spelling)
			continue
		}
		if got := k.String(); got != spelling {
			t.Errorf("LookupKeyword(%q) = kind %q, want %q", spelling, got, spelling)
		}
	}
}

func TestRemovedKeywordsAreOrdinaryIdentifiers(t *testing.T) {
	for _, text := range []string{"while", "loop", "unreachable", "export", "let", "mut", "as"} {
		if _, ok := LookupKeyword(text); ok {
			t.Errorf("LookupKeyword(%q) reported a keyword", text)
		}
	}
}

func TestEveryKindHasAName(t *testing.T) {
	for k := Kind(0); k < NumKinds; k++ {
		if k.String() == "" {
			t.Errorf("kind %d has an empty name", k)
		}
	}
	if got, want := Kind(200).String(), "kind(200)"; got != want {
		t.Errorf("Kind(200).String() = %q, want %q", got, want)
	}
}

func TestOperatorSpellings(t *testing.T) {
	cases := map[Kind]string{
		Plus: "+", Ne: "!=", Shr: ">>", ShrAssign: ">>=", Range: "..",
		FatArrow: "=>", Arrow: "->", Ellipsis: "...", LBrace: "{",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestNumSuffixClassification(t *testing.T) {
	if SuffixNone.String() != "" {
		t.Errorf("SuffixNone.String() = %q, want empty", SuffixNone.String())
	}
	for _, s := range []NumSuffix{SuffixI8, SuffixU64, SuffixUsize} {
		if !s.IsInt() || s.IsFloat() {
			t.Errorf("suffix %q is not classified as integer", s)
		}
	}
	for _, s := range []NumSuffix{SuffixF32, SuffixF64} {
		if s.IsInt() || !s.IsFloat() {
			t.Errorf("suffix %q is not classified as float", s)
		}
	}
	if SuffixNone.IsInt() || SuffixNone.IsFloat() {
		t.Error("SuffixNone must not classify as a numeric suffix")
	}
	if got, want := SuffixUsize.String(), "usize"; got != want {
		t.Errorf("SuffixUsize.String() = %q, want %q", got, want)
	}
}
