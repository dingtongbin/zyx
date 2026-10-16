package toolchain

import "testing"

func TestParseTarget(t *testing.T) {
	cases := []struct {
		triple string
		arch   string
		os     string
	}{
		{"amd64-unknown-linux-gnu", "amd64", "linux"},
		{"x86_64-unknown-linux-gnu", "amd64", "linux"},
		{"arm64-apple-darwin", "arm64", "darwin"},
		{"aarch64-apple-ios", "arm64", "ios"},
		{"amd64-pc-windows-msvc", "amd64", "windows"},
		{"wasm32-unknown-unknown", "wasm32", "wasm"},
		{"arm64-unknown-harmony", "arm64", "harmony"},
		{"arm64-unknown-android", "arm64", "android"},
		{"i686-unknown-linux-gnu", "i386", "linux"},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.triple)
		if err != nil {
			t.Errorf("%s: %v", c.triple, err)
			continue
		}
		if got.Arch != c.arch || got.OS != c.os {
			t.Errorf("%s: arch/os = %s/%s, want %s/%s",
				c.triple, got.Arch, got.OS, c.arch, c.os)
		}
	}
}

func TestParseTargetCanonicalizes(t *testing.T) {
	got, err := ParseTarget("x86_64-unknown-linux-gnu")
	if err != nil {
		t.Fatal(err)
	}
	if got.Triple != "amd64-unknown-linux-gnu" {
		t.Errorf("triple = %q, want amd64-unknown-linux-gnu", got.Triple)
	}
	if got.Env != "gnu" {
		t.Errorf("env = %q, want gnu", got.Env)
	}
}

func TestParseTargetErrors(t *testing.T) {
	for _, triple := range []string{
		"",
		"linux",
		"amd64-unknown",
		"sparc-unknown-linux",
		"amd64-unknown-plan9",
	} {
		if _, err := ParseTarget(triple); err == nil {
			t.Errorf("%q must not parse", triple)
		}
	}
}

func TestLayoutOfTargets(t *testing.T) {
	cases := map[string]uint64{
		"amd64-unknown-linux-gnu": 8,
		"arm64-apple-darwin":      8,
		"wasm32-unknown-unknown":  4,
		"i686-unknown-linux-gnu":  4,
	}
	for triple, ptr := range cases {
		got, err := ParseTarget(triple)
		if err != nil {
			t.Fatalf("%s: %v", triple, err)
		}
		if got.Layout().PtrSize != ptr {
			t.Errorf("%s: pointer size = %d, want %d", triple, got.Layout().PtrSize, ptr)
		}
	}
}

func TestHostTarget(t *testing.T) {
	h := HostTarget()
	if h.Arch == "" || h.OS == "" || h.Triple == "" {
		t.Fatalf("host target = %+v, want every segment", h)
	}
	if h.Layout().PtrSize == 0 {
		t.Error("the host target must have a pointer size")
	}
}

func TestLoopBackendPerPlatform(t *testing.T) {
	// Every platform of the matrix has an event loop backend, so a new
	// platform cannot be added without one.
	tc := &Toolchain{}
	want := map[string]string{
		"linux":   "loop_epoll.c",
		"android": "loop_epoll.c",
		"harmony": "loop_epoll.c",
		"darwin":  "loop_kqueue.c",
		"ios":     "loop_kqueue.c",
		"windows": "loop_winsock.c",
		"wasm":    "loop_wasi.c",
	}
	for _, os := range OperatingSystems() {
		name, ok := want[os]
		if !ok {
			t.Errorf("the platform matrix lists %s and the test does not", os)
			continue
		}
		if got := tc.loopBackend(Target{OS: os}); got != name {
			t.Errorf("the event loop of %s is %q, want %q", os, got, name)
		}
	}
}

func TestPlatformMatrix(t *testing.T) {
	if len(OperatingSystems()) == 0 || len(Architectures()) == 0 {
		t.Fatal("the platform matrix must not be empty")
	}
	for _, os := range OperatingSystems() {
		if !isSupportedOS(os) {
			t.Errorf("%s is listed but not supported", os)
		}
	}
	if !isSupportedOS("wasm") || isSupportedOS("plan9") {
		t.Error("the operating system set is wrong")
	}
}

// TestRecordArgumentWindows checks the Windows rule: a record of one, two,
// four, or eight bytes travels as an integer of that width, and every other
// one travels as a copy.
func TestRecordArgumentWindows(t *testing.T) {
	win := Target{Arch: "amd64", OS: "windows"}
	for _, size := range []uint64{1, 2, 4, 8} {
		got := win.RecordArgument(RecordShape{Size: size, Align: size})
		if len(got) != 1 || got[0].Class != RecordInteger || got[0].Width != size*8 {
			t.Errorf("a record of %d bytes = %+v, want one integer of %d bits", size, got, size*8)
		}
	}
	for _, size := range []uint64{3, 12, 16, 24} {
		got := win.RecordArgument(RecordShape{Size: size, Align: 8})
		if len(got) != 1 || got[0].Class != RecordCopy {
			t.Errorf("a record of %d bytes = %+v, want a copy", size, got)
		}
	}
}

// TestRecordArgumentSysV checks the System V rule against the placement clang
// gives the same records: one value per eightbyte, a vector register when the
// eightbyte holds floating-point fields only, a general register otherwise,
// and a copy past two eightbytes.
func TestRecordArgumentSysV(t *testing.T) {
	linux := Target{Arch: "amd64", OS: "linux"}
	i32 := func(offset uint64) RecordField { return RecordField{Offset: offset, Size: 4} }
	f64 := func(offset uint64) RecordField { return RecordField{Offset: offset, Size: 8, Float: true} }
	f32 := func(offset uint64) RecordField { return RecordField{Offset: offset, Size: 4, Float: true} }
	cases := []struct {
		name   string
		shape  RecordShape
		class  []RecordClass
		offset []uint64
	}{
		{"two ints", RecordShape{Size: 8, Align: 4, Fields: []RecordField{i32(0), i32(4)}},
			[]RecordClass{RecordInteger}, []uint64{0}},
		{"two doubles", RecordShape{Size: 16, Align: 8, Fields: []RecordField{f64(0), f64(8)}},
			[]RecordClass{RecordFloat, RecordFloat}, []uint64{0, 8}},
		{"an int and a double", RecordShape{Size: 16, Align: 8, Fields: []RecordField{i32(0), f64(8)}},
			[]RecordClass{RecordInteger, RecordFloat}, []uint64{0, 8}},
		{"three chars", RecordShape{Size: 3, Align: 1, Fields: []RecordField{{Offset: 0, Size: 1}, {Offset: 1, Size: 1}, {Offset: 2, Size: 1}}},
			[]RecordClass{RecordInteger}, []uint64{0}},
		{"two floats", RecordShape{Size: 8, Align: 4, Fields: []RecordField{f32(0), f32(4)}},
			[]RecordClass{RecordFloat}, []uint64{0}},
		{"a pointer and a count", RecordShape{Size: 16, Align: 8, Fields: []RecordField{{Offset: 0, Size: 8}, {Offset: 8, Size: 8}}},
			[]RecordClass{RecordInteger, RecordInteger}, []uint64{0, 8}},
		{"three longs", RecordShape{Size: 24, Align: 8, Fields: []RecordField{{Offset: 0, Size: 8}, {Offset: 8, Size: 8}, {Offset: 16, Size: 8}}},
			[]RecordClass{RecordCopy}, []uint64{0}},
	}
	for _, c := range cases {
		got := linux.RecordArgument(c.shape)
		if len(got) != len(c.class) {
			t.Errorf("%s: %d values, want %d", c.name, len(got), len(c.class))
			continue
		}
		for i := range got {
			if got[i].Class != c.class[i] || got[i].Offset != c.offset[i] {
				t.Errorf("%s value %d = %+v, want class %v at offset %d", c.name, i, got[i], c.class[i], c.offset[i])
			}
		}
	}
}

// TestRecordArgumentSysVUnaligned checks that a field the target's rules
// would not place naturally sends the whole record through a copy.
func TestRecordArgumentSysVUnaligned(t *testing.T) {
	linux := Target{Arch: "amd64", OS: "linux"}
	shape := RecordShape{Size: 9, Align: 1, Fields: []RecordField{{Offset: 0, Size: 1}, {Offset: 1, Size: 8}}}
	got := linux.RecordArgument(shape)
	if len(got) != 1 || got[0].Class != RecordCopy {
		t.Errorf("an unaligned record = %+v, want a copy", got)
	}
}
