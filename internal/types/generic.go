// Type parameters, substitution, and generic instantiation.

package types

// Substitute returns t with every type parameter replaced by its binding.
// A parameter without a binding is left in place, so substitution can run
// on a partially instantiated type.
func Substitute(t *Type, bindings map[string]*Type) *Type {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case KindTypeParam:
		if bound, ok := bindings[t.ID]; ok {
			return bound
		}
		return t
	case KindSlice:
		return SliceOf(Substitute(t.Elem, bindings))
	case KindArray:
		return ArrayOf(Substitute(t.Elem, bindings), t.Len)
	case KindChan:
		return ChanOf(Substitute(t.Elem, bindings))
	case KindTuple:
		return TupleOf(substituteList(t.Elems, bindings)...)
	case KindFunc:
		return FuncOf(Substitute(t.Result, bindings), substituteList(t.Elems, bindings)...)
	}
	if t.IsNamed() && len(t.Args) > 0 {
		out := *t
		out.Args = substituteList(t.Args, bindings)
		return &out
	}
	return t
}

// substituteList applies Substitute to a type list.
func substituteList(list []*Type, bindings map[string]*Type) []*Type {
	if len(list) == 0 {
		return nil
	}
	out := make([]*Type, len(list))
	for i, t := range list {
		out[i] = Substitute(t, bindings)
	}
	return out
}

// ContainsTypeParam reports whether t mentions a type parameter.
func ContainsTypeParam(t *Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case KindTypeParam:
		return true
	case KindSlice, KindArray, KindChan:
		return ContainsTypeParam(t.Elem)
	case KindTuple:
		return containsParam(t.Elems)
	case KindFunc:
		return containsParam(t.Elems) || ContainsTypeParam(t.Result)
	}
	return containsParam(t.Args)
}

// containsParam reports whether any type in a list mentions a parameter.
func containsParam(list []*Type) bool {
	for _, t := range list {
		if ContainsTypeParam(t) {
			return true
		}
	}
	return false
}

// ParamIDs returns the ids of every type parameter t mentions, in first
// appearance order. A caller can use it to collect the type arguments of an
// instantiation from a concrete type.
func ParamIDs(t *Type) []string {
	var out []string
	seen := map[string]bool{}
	var visit func(*Type)
	visit = func(t *Type) {
		if t == nil {
			return
		}
		switch t.Kind {
		case KindTypeParam:
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t.ID)
			}
			return
		case KindSlice, KindArray, KindChan:
			visit(t.Elem)
			return
		case KindTuple:
			for _, e := range t.Elems {
				visit(e)
			}
			return
		case KindFunc:
			for _, e := range t.Elems {
				visit(e)
			}
			visit(t.Result)
			return
		}
		for _, a := range t.Args {
			visit(a)
		}
	}
	visit(t)
	return out
}

// Bindings returns a substitution that maps the parameters of pattern onto
// the corresponding parts of actual, matching the two types structurally.
// It reports false when the shapes differ, which means the instantiation
// does not type-check.
func Bindings(pattern, actual *Type) (map[string]*Type, bool) {
	bindings := map[string]*Type{}
	if !bind(pattern, actual, bindings) {
		return nil, false
	}
	return bindings, true
}

// bind matches one pattern type against one actual type.
func bind(pattern, actual *Type, out map[string]*Type) bool {
	if pattern == nil || actual == nil {
		return pattern == actual
	}
	if pattern.Kind == KindTypeParam {
		if prev, ok := out[pattern.ID]; ok {
			return Identical(prev, actual)
		}
		out[pattern.ID] = actual
		return true
	}
	if pattern.Kind != actual.Kind {
		return false
	}
	switch pattern.Kind {
	case KindSlice:
		return bind(pattern.Elem, actual.Elem, out)
	case KindArray:
		return pattern.Len == actual.Len && bind(pattern.Elem, actual.Elem, out)
	case KindChan:
		return bind(pattern.Elem, actual.Elem, out)
	case KindTuple, KindFunc:
		if len(pattern.Elems) != len(actual.Elems) {
			return false
		}
		for i := range pattern.Elems {
			if !bind(pattern.Elems[i], actual.Elems[i], out) {
				return false
			}
		}
		if pattern.Kind == KindFunc {
			return bind(pattern.Result, actual.Result, out)
		}
		return true
	}
	if pattern.IsNamed() {
		if pattern.ID != actual.ID || len(pattern.Args) != len(actual.Args) {
			return false
		}
		for i := range pattern.Args {
			if !bind(pattern.Args[i], actual.Args[i], out) {
				return false
			}
		}
		return true
	}
	return true
}
