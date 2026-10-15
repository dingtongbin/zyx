// Target triple and data layout handling.

package llvmgen

import "zyx/internal/toolchain"

// llvmTriple returns the spelling LLVM uses for a target. The target table
// owns it, because the layout, the C ABI shape, and the triple are one fact
// about a platform and must not be able to disagree.
func llvmTriple(t toolchain.Target) string { return t.LLVMTriple() }
