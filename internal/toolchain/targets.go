// Target triples and the platform matrix.

package toolchain

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"zyx/internal/layout"
	"zyx/internal/types"
)

// A Target is one resolved target: the triple the .zyxc layer records, and
// the platform segments that select files and decide layout properties.
type Target struct {
	// Triple is the canonical triple string.
	Triple string
	// Arch is the architecture segment, spelled as in Go: amd64, arm64,
	// i386, arm, wasm32.
	Arch string
	// OS is the operating system segment, spelled as in Go: linux,
	// windows, darwin, android, ios, harmony, wasm.
	OS string
	// Env is the environment segment, empty when the triple omits it.
	Env string
}

// supportedOS lists the operating systems the compiler targets.
var supportedOS = []string{"linux", "windows", "darwin", "android", "ios", "harmony", "wasm"}

// archLayout gives the layout properties of each supported architecture.
var archLayout = map[string]layout.Target{
	"amd64":  {PtrSize: 8, PtrAlign: 8},
	"arm64":  {PtrSize: 8, PtrAlign: 8},
	"i386":   {PtrSize: 4, PtrAlign: 4},
	"arm":    {PtrSize: 4, PtrAlign: 4},
	"wasm32": {PtrSize: 4, PtrAlign: 4},
}

// archAliases maps the spellings a target triple may use for an
// architecture onto the segment the platform matrix and the file suffixes
// use, which follows Go.
var archAliases = map[string]string{
	"x86_64":  "amd64",
	"aarch64": "arm64",
	"i686":    "i386",
	"i586":    "i386",
	"armv7":   "arm",
	"armv7a":  "arm",
}

// ParseTarget parses a target triple of the form arch-vendor-os[-env]. The
// architecture segment is canonicalized, so a triple written with an LLVM
// spelling names the same target as one written with the platform name.
func ParseTarget(triple string) (Target, error) {
	parts := strings.Split(triple, "-")
	if len(parts) < 3 {
		return Target{}, fmt.Errorf("target %q is not of the form arch-vendor-os", triple)
	}
	arch := canonicalArch(parts[0])
	os := parts[2]
	if _, ok := archLayout[arch]; !ok {
		return Target{}, fmt.Errorf("target %q has unknown architecture %q", triple, parts[0])
	}
	// A WebAssembly triple names its operating system after the
	// environment, which the platform matrix calls wasm.
	if os == "unknown" || os == "wasi" {
		if arch != "wasm32" {
			return Target{}, fmt.Errorf("target %q has unknown operating system %q", triple, os)
		}
		os = "wasm"
	}
	if !isSupportedOS(os) {
		return Target{}, fmt.Errorf("target %q has unknown operating system %q", triple, os)
	}
	t := Target{Triple: arch + "-" + parts[1] + "-" + os, Arch: arch, OS: os}
	if len(parts) > 3 {
		t.Env = parts[3]
		t.Triple += "-" + parts[3]
	}
	return t, nil
}

// canonicalArch returns the platform name of an architecture segment.
func canonicalArch(arch string) string {
	if alias, ok := archAliases[arch]; ok {
		return alias
	}
	return arch
}

// isSupportedOS reports whether an operating system segment is in the
// platform matrix.
func isSupportedOS(os string) bool {
	for _, known := range supportedOS {
		if known == os {
			return true
		}
	}
	return false
}

// OperatingSystems lists the supported operating system segments.
func OperatingSystems() []string {
	out := append([]string(nil), supportedOS...)
	sort.Strings(out)
	return out
}

// Architectures lists the supported architecture segments.
func Architectures() []string {
	out := make([]string, 0, len(archLayout))
	for arch := range archLayout {
		out = append(out, arch)
	}
	sort.Strings(out)
	return out
}

// HostTarget returns the target of the machine running the compiler.
func HostTarget() Target {
	arch := runtime.GOARCH
	if arch == "386" {
		arch = "i386"
	}
	os := runtime.GOOS
	return Target{
		Triple: hostTriple(arch, os),
		Arch:   arch,
		OS:     os,
		Env:    hostEnv(os),
	}
}

// hostTriple builds the triple of a host platform.
func hostTriple(arch, os string) string {
	vendor := "unknown"
	switch os {
	case "windows":
		vendor = "pc"
	case "darwin", "ios":
		vendor = "apple"
	case "wasm":
		return arch + "-unknown-unknown"
	}
	triple := arch + "-" + vendor + "-" + os
	if env := hostEnv(os); env != "" {
		triple += "-" + env
	}
	return triple
}

// hostEnv returns the environment segment a host triple uses.
func hostEnv(os string) string {
	if os == "windows" {
		return "msvc"
	}
	return ""
}

// Layout returns the layout properties of the target.
func (t Target) Layout() layout.Target {
	if l, ok := archLayout[t.Arch]; ok {
		return l
	}
	return layout.Target{PtrSize: 8, PtrAlign: 8}
}

// LLVMTriple returns the spelling LLVM uses for the target. The platform
// matrix spells architectures as Go does, and LLVM spells them the way its
// own targets are named. This is the one place a new platform is added: the
// layout above, this triple, and the C ABI shape all follow from it.
func (t Target) LLVMTriple() string {
	arch := t.Arch
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	case "i386":
		arch = "i686"
	case "arm":
		arch = "armv7"
	}
	switch t.OS {
	case "windows":
		return arch + "-pc-windows-msvc"
	case "darwin":
		return arch + "-apple-darwin"
	case "ios":
		return arch + "-apple-ios"
	case "android":
		return arch + "-unknown-linux-android"
	case "wasm":
		return arch + "-unknown-wasi"
	default:
		return arch + "-unknown-linux-gnu"
	}
}

// PtrInt returns the LLVM integer that holds a pointer-sized value.
func (t Target) PtrInt() string {
	if t.Layout().PtrSize == 4 {
		return "i32"
	}
	return "i64"
}

// ABIKind classifies how a value of a type crosses the C boundary.
type ABIKind uint8

const (
	// ABIScalar passes the value itself.
	ABIScalar ABIKind = iota
	// ABIFatPointer passes an address and a length.
	ABIFatPointer
	// ABIAggregate passes the address of an object.
	ABIAggregate
)

// ABIKind returns how a value of a type crosses the C boundary. The target
// table owns the classification, because it is part of the platform's ABI;
// the backend spells it in LLVM types.
func (t Target) ABIKind(ty *types.Type) ABIKind {
	switch ty.Kind {
	case types.KindStr, types.KindSlice:
		return ABIFatPointer
	case types.KindInterface:
		return ABIFatPointer
	case types.KindStruct, types.KindExternStruct, types.KindUnion,
		types.KindEnum, types.KindArray:
		return ABIAggregate
	}
	return ABIScalar
}
