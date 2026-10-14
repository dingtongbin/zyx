// Target triple and data layout handling.

package llvmgen

import "zyx/internal/toolchain"

// llvmTriple maps a platform triple onto the spelling LLVM uses. The
// platform matrix spells architectures as Go does, and LLVM spells them the
// way its own targets are named.
func llvmTriple(t toolchain.Target) string {
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
