// Instance deduplication by layout signature.

package mono

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// instanceName returns the name an instance is declared by: the generic
// name and the first sixteen hexadecimal digits of its signature.
func instanceName(name string, args []*types.Type, target string) string {
	return name + "$$" + signature(args, target)
}

// signature hashes the type arguments and the target, which is what
// separates one instance from another. Two uses that agree on both share
// one instance.
func signature(args []*types.Type, target string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, zyxc.TypeText(a, ""))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, ", ") + "@" + target))
	return hex.EncodeToString(sum[:])[:16]
}
