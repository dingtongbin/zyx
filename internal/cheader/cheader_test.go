package cheader

import (
	"os"
	"path/filepath"
	"testing"

	"zyx/internal/toolchain"
	"zyx/internal/types"
)

// shapes is a header that carries one declaration of every kind the table
// reports.
const shapes = `typedef unsigned long long size_t;
struct Point { int x; int y; };
union Value { int i; double d; };
enum Color { Red = 1, Green = 2 };
typedef int (*Callback)(int, void *);
int add(int a, int b);
void fill(const char *p, size_t n);
double scale(double v, float f);
struct Point *origin(void);
int apply(Callback cb, int v);
void log(const char *fmt, ...);
extern int global_counter;
`

// TestParseReadsTheDeclarations checks that a header's declarations come
// through as the types the language crosses them as.
func TestParseReadsTheDeclarations(t *testing.T) {
	tc, err := toolchain.Locate()
	if err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dir := t.TempDir()
	header := filepath.Join(dir, "shapes.h")
	if err := os.WriteFile(header, []byte(shapes), 0o644); err != nil {
		t.Fatal(err)
	}
	table, err := Parse(tc.Clang, header, []string{dir})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		params []string
		result string
	}{
		{"add", []string{"i32", "i32"}, "i32"},
		{"fill", []string{"raw", "u64"}, ""},
		{"scale", []string{"f64", "f32"}, "f64"},
		{"origin", nil, "raw"},
		{"apply", []string{"raw", "i32"}, "i32"},
	}
	for _, c := range cases {
		fn, ok := table.Functions[c.name]
		if !ok {
			t.Errorf("%s is missing from the table", c.name)
			continue
		}
		got := make([]string, 0, len(fn.Params))
		for _, pt := range fn.Params {
			got = append(got, text(pt))
		}
		if len(got) != len(c.params) {
			t.Errorf("%s: %d parameters, want %d", c.name, len(got), len(c.params))
		} else {
			for i := range got {
				if got[i] != c.params[i] {
					t.Errorf("%s: parameter %d is %s, want %s", c.name, i, got[i], c.params[i])
				}
			}
		}
		if result := text(fn.Result); result != c.result {
			t.Errorf("%s: result is %s, want %s", c.name, result, c.result)
		}
	}

	if fn := table.Functions["log"]; fn == nil || !fn.Variadic {
		t.Error("log must be variadic")
	}
	if record, ok := table.Records["Point"]; !ok || record.Union {
		t.Error("Point must be a struct")
	}
	if record, ok := table.Records["Value"]; !ok || !record.Union {
		t.Error("Value must be a union")
	}
	if got := table.Constants["Red"]; got != 1 {
		t.Errorf("Red = %d, want 1", got)
	}
	if got := table.Constants["Green"]; got != 2 {
		t.Errorf("Green = %d, want 2", got)
	}
	// A C function pointer is an address with no receiver, which is what raw
	// is, so a callback field needs no type of its own.
	if got := text(table.Aliases["Callback"]); got != "raw" {
		t.Errorf("Callback is %s, want raw", got)
	}
	if got := text(table.Globals["global_counter"]); got != "i32" {
		t.Errorf("global_counter is %s, want i32", got)
	}
}

// text renders a type the way a test compares it, and the empty string for
// the absence of one.
func text(t *types.Type) string {
	if t == nil {
		return ""
	}
	return t.String()
}
