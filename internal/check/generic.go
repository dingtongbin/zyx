// Constraint checking for generic bodies.

package check

import "zyx/internal/types"

// paramMethod returns the signature of the method a constrained type
// parameter may call, or nil when no constraint declares it. The method comes
// from one of the interfaces the parameter requires, with Self standing for
// the parameter itself, so a generic body is checked once against the
// constraint rather than once per instantiation.
func (p *Package) paramMethod(recv *types.Type, name string) *types.Type {
	for _, iface := range recv.Constraints {
		if sig, ok := p.interfaceMethods(iface, recv)[name]; ok {
			return sig
		}
	}
	return nil
}
