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
		// An interface the language provides has no declaration to read, so
		// its methods are the ones the language gives the types it accepts.
		if sig := builtinIfaceMethod(iface, recv, name); sig != nil {
			return sig
		}
	}
	return nil
}

// builtinIfaceMethod returns the signature of a method one of the interfaces
// the language provides declares, with the receiver in place of Self. The
// language gives those methods to every type it accepts for the interface, so
// a constrained parameter reaches them the way a concrete value does.
func builtinIfaceMethod(iface, recv *types.Type, name string) *types.Type {
	if iface == nil || iface.Kind != types.KindInterface || iface.Pkg != "" {
		return nil
	}
	switch iface.Name {
	case "Eq":
		if name == "Eq" {
			return types.FuncOf(types.Bool, recv)
		}
	case "Hash":
		if name == "Hash" {
			return types.FuncOf(types.Uint64)
		}
	case "Show":
		if name == "Show" {
			return types.FuncOf(nil, types.Buf)
		}
	case "Ord":
		if name == "Compare" {
			return types.FuncOf(types.Int32, recv)
		}
	}
	return nil
}
