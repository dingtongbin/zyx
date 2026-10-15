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
