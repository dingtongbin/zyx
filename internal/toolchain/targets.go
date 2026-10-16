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

// RecordClass is the class of one value a record contributes at a call.
type RecordClass uint8

const (
	// RecordInteger travels in a general register.
	RecordInteger RecordClass = iota
	// RecordFloat travels in a vector register.
	RecordFloat
	// RecordCopy travels as the address of a copy the caller owns, which is
	// how a record too large or too oddly shaped for registers travels.
	RecordCopy
)

// RecordArg is one argument a record contributes, and where in the record the
// value is read from.
type RecordArg struct {
	// Class is the class of the value.
	Class RecordClass
	// Offset is the byte offset of the value in the record.
	Offset uint64
	// Width is the width in bits of the integer the value travels as. It is
	// zero for a floating-point value, which travels as a double.
	Width uint64
	// Align is the alignment of a copy in bytes, for a RecordCopy.
	Align uint64
}

// RecordShape describes a record to the target's argument placement rules.
type RecordShape struct {
	// Size is the size of the record in bytes.
	Size uint64
	// Align is the alignment of the record in bytes.
	Align uint64
	// Fields describes every field, in declaration order.
	Fields []RecordField
}

// RecordField describes one field as the placement rules see it. A nested
// record contributes its own fields, at the offsets they land at.
type RecordField struct {
	// Offset is the field's byte offset in the outer record.
	Offset uint64
	// Size is the field's size in bytes.
	Size uint64
	// Float marks a floating-point field.
	Float bool
}

// RecordArgument returns the arguments a record contributes as an argument on
// the target. A C compiler places a record by rules of its own, and the two
// families of 64-bit targets place it differently, so the target table owns
// the rule.
//
// Windows passes a record of one, two, four, or eight bytes as an integer of
// that width and every other one through a copy. A System V target classifies
// each eightbyte of the record: one that holds only floating-point fields
// travels in a vector register and one that holds anything else travels in a
// general register, while a record larger than two eightbytes, or one with a
// field that is not naturally aligned, travels through a copy.
func (t Target) RecordArgument(s RecordShape) []RecordArg {
	if t.OS == "windows" {
		switch s.Size {
		case 1, 2, 4, 8:
			return []RecordArg{{Class: RecordInteger, Width: s.Size * 8}}
		}
		return []RecordArg{{Class: RecordCopy, Align: s.Align}}
	}
	if s.Size > 16 || unalignedFields(s) {
		return []RecordArg{{Class: RecordCopy, Align: s.Align}}
	}
	var out []RecordArg
	for start := uint64(0); start < s.Size; start += 8 {
		out = append(out, eightbyte(s, start))
	}
	return out
}

// eightbyte classifies the eightbyte that starts at start. The class is what
// decides the register file; the width is how many bytes of the record the
// value occupies, so a read of it never reaches past the record.
func eightbyte(s RecordShape, start uint64) RecordArg {
	end := start + 8
	if end > s.Size {
		end = s.Size
	}
	arg := RecordArg{Class: RecordFloat, Offset: start, Width: (end - start) * 8}
	for _, f := range s.Fields {
		if f.Offset >= end || f.Offset+f.Size <= start {
			continue
		}
		if !f.Float {
			// One integer or pointer field makes the whole eightbyte an
			// integer, because the register has one class.
			arg.Class = RecordInteger
			break
		}
	}
	return arg
}

// unalignedFields reports whether a field sits where the target's rules would
// not put it, which the System V rules answer with a copy.
func unalignedFields(s RecordShape) bool {
	for _, f := range s.Fields {
		align := uint64(1)
		for align < f.Size && align < 8 {
			align *= 2
		}
		if f.Offset%align != 0 {
			return true
		}
	}
	return false
}

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
