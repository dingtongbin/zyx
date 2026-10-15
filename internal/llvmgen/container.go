// Containers: the operations on an opaque handle. An element crosses as the
// address of a value of its type, so the runtime moves bytes and the backend
// decides what those bytes mean.

package llvmgen

import (
	"fmt"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// containerCall lowers one operation on a container.
func (g *generator) containerCall(module string, n *zyxc.Node, recv *value, name string) *value {
	switch recv.typ.Name {
	case "Array":
		return g.arrayCall(module, n, recv, name)
	case "Map":
		return g.mapCall(module, n, recv, name)
	case "Set":
		return g.setCall(module, n, recv, name)
	}
	g.fail(fmt.Errorf("llvmgen: %s.%s is not lowered", recv.typ.Name, name))
	return nil
}

// newContainer creates a container. Its buffer belongs to the region of the
// running coroutine, so it is released with the region that made it.
func (g *generator) newContainer(module string, n *zyxc.Node, typ *types.Type) *value {
	if typ.Name == "Map" {
		key, val := arg(typ, 0), arg(typ, 1)
		keySize, ok := g.arraySize(key)
		if !ok {
			return nil
		}
		valSize, ok := g.arraySize(val)
		if !ok {
			return nil
		}
		hash, eq := g.mapHash(key)
		if hash == "" {
			return nil
		}
		g.declareRuntime("zyxrt_map_new", "ptr", g.ptrInt(), g.ptrInt(), "ptr", "ptr")
		reg := g.newReg()
		g.emit("  %s = call ptr @zyxrt_map_new(%s %d, %s %d, ptr %s, ptr %s)",
			reg, g.ptrInt(), keySize, g.ptrInt(), valSize, hash, eq)
		return &value{text: reg, ll: "ptr", typ: typ}
	}
	if typ.Name == "Set" {
		elem := arg(typ, 0)
		size, ok := g.arraySize(elem)
		if !ok {
			return nil
		}
		hash, eq := g.mapHash(elem)
		if hash == "" {
			return nil
		}
		g.declareRuntime("zyxrt_set_new", "ptr", g.ptrInt(), "ptr", "ptr")
		reg := g.newReg()
		g.emit("  %s = call ptr @zyxrt_set_new(%s %d, ptr %s, ptr %s)",
			reg, g.ptrInt(), size, hash, eq)
		return &value{text: reg, ll: "ptr", typ: typ}
	}
	if typ.Name != "Array" {
		g.fail(fmt.Errorf("llvmgen: new %s is not lowered", typ))
		return nil
	}
	elem := arg(typ, 0)
	size, ok := g.arraySize(elem)
	if !ok {
		return nil
	}
	capacity := "0"
	if a := g.sole(n.First("arg")); a != nil {
		v := g.load(g.expr(module, a, types.Usize))
		if v == nil {
			return nil
		}
		capacity = v.text
	}
	g.declareRuntime("zyxrt_array_new", "ptr", g.ptrInt(), g.ptrInt())
	reg := g.newReg()
	g.emit("  %s = call ptr @zyxrt_array_new(%s %d, %s %s)", reg, g.ptrInt(), size, g.ptrInt(), capacity)
	return &value{text: reg, ll: "ptr", typ: typ}
}

// arrayCall lowers one operation on an Array.
func (g *generator) arrayCall(module string, n *zyxc.Node, recv *value, name string) *value {
	elem := arg(recv.typ, 0)
	switch name {
	case "Len":
		g.declareRuntime("zyxrt_array_len", g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @zyxrt_array_len(ptr %s)", reg, g.ptrInt(), recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Push":
		slot, ok := g.elementSlot(module, n, elem)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_array_push", "void", "ptr", "ptr")
		g.emit("  call void @zyxrt_array_push(ptr %s, ptr %s)", recv.text, slot)
		return &value{ll: "void"}
	case "Remove":
		index := g.load(g.expr(module, g.sole(n.First("arg")), types.Usize))
		if index == nil {
			return nil
		}
		g.declareRuntime("zyxrt_array_remove", "void", "ptr", g.ptrInt())
		g.emit("  call void @zyxrt_array_remove(ptr %s, %s %s)", recv.text, index.ll, index.text)
		return &value{ll: "void"}
	case "Pop":
		return g.arrayTake(module, n, recv, elem, "zyxrt_array_pop", "")
	case "Get":
		return g.arrayTake(module, n, recv, elem, "zyxrt_array_get", "index")
	case "get_unchecked":
		index := g.load(g.expr(module, g.sole(n.First("arg")), types.Usize))
		if index == nil {
			return nil
		}
		slot := g.alloca(elem)
		g.declareRuntime("zyxrt_array_get_unchecked", "void", "ptr", g.ptrInt(), "ptr")
		g.emit("  call void @zyxrt_array_get_unchecked(ptr %s, %s %s, ptr %s)",
			recv.text, index.ll, index.text, slot)
		return g.load(&value{text: slot, ll: g.llPass(elem), typ: elem, lvalue: true, inline: true})
	}
	g.fail(fmt.Errorf("llvmgen: Array.%s is not lowered", name))
	return nil
}

// mapCall lowers one operation on a Map.
func (g *generator) mapCall(module string, n *zyxc.Node, recv *value, name string) *value {
	key, val := arg(recv.typ, 0), arg(recv.typ, 1)
	switch name {
	case "Len":
		g.declareRuntime("zyxrt_map_len", g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @zyxrt_map_len(ptr %s)", reg, g.ptrInt(), recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Set":
		keySlot, ok := g.elementSlot(module, n, key)
		if !ok {
			return nil
		}
		valSlot, ok := g.elementSlotAt(module, n, val, 1)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_map_set", "void", "ptr", "ptr", "ptr")
		g.emit("  call void @zyxrt_map_set(ptr %s, ptr %s, ptr %s)", recv.text, keySlot, valSlot)
		return &value{ll: "void"}
	case "Contains":
		keySlot, ok := g.elementSlot(module, n, key)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_map_contains", "i32", "ptr", "ptr")
		reg := g.newReg()
		g.emit("  %s = call i32 @zyxrt_map_contains(ptr %s, ptr %s)", reg, recv.text, keySlot)
		return g.boolFrom(reg)
	case "Get", "Remove":
		keySlot, ok := g.elementSlot(module, n, key)
		if !ok {
			return nil
		}
		return g.mapTake(module, n, recv, keySlot, val, name == "Remove")
	case "Keys", "Values":
		elem := key
		if name == "Values" {
			elem = val
		}
		return g.mapSlice(module, n, recv, elem, name == "Values")
	case "KeyList":
		return g.mapKeyList(module, n, recv, key)
	}
	g.fail(fmt.Errorf("llvmgen: Map.%s is not lowered", name))
	return nil
}

// mapTake lowers Get and Remove, which yield an Option of the value.
func (g *generator) mapTake(module string, n *zyxc.Node, recv *value, keySlot string, val *types.Type, remove bool) *value {
	slot := g.alloca(val)
	fn := "zyxrt_map_get"
	if remove {
		fn = "zyxrt_map_remove"
	}
	g.declareRuntime(fn, "i32", "ptr", "ptr", "ptr")
	flag := g.newReg()
	g.emit("  %s = call i32 @%s(ptr %s, ptr %s, ptr %s)", flag, fn, recv.text, keySlot, slot)
	return g.optionValue(module, n, slot, val, flag)
}

// optionValue builds the Option a lookup answers with.
func (g *generator) optionValue(module string, n *zyxc.Node, slot string, elem *types.Type, flag string) *value {
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil || typ.Kind != types.KindEnum || len(typ.Args) == 0 {
		g.fail(fmt.Errorf("llvmgen: the result of the lookup is not settled"))
		return nil
	}
	some, _, ok := g.variantOf(typ, "Some")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant Some", typ))
		return nil
	}
	none, _, ok := g.variantOf(typ, "None")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant None", typ))
		return nil
	}
	obj := g.allocateIn(n, typ)
	tagType := g.tagType(g.tagSize(typ))
	present := g.newReg()
	g.emit("  %s = icmp ne i32 %s, 0", present, flag)
	tag := g.newReg()
	g.emit("  %s = select i1 %s, %s %d, %s %d", tag, present, tagType, some, tagType, none)
	g.emit("  store %s %s, ptr %s", tagType, tag, obj)
	someLabel := g.newLabel("some")
	endLabel := g.newLabel("some.end")
	g.terminator("  br i1 %s, label %%%s, label %%%s", present, someLabel, endLabel)
	g.label(someLabel)
	element := g.load(&value{text: slot, ll: g.llPass(elem), typ: elem, lvalue: true, inline: true})
	g.storeInto(g.payloadElement(obj, typ, []*types.Type{elem}, 0), elem, element, true)
	g.terminator("  br label %%%s", endLabel)
	g.label(endLabel)
	return &value{text: obj, ll: "ptr", typ: typ}
}

// mapSlice lowers Keys and Values, which yield a slice of the keys or of the
// values in the order the map holds them.
func (g *generator) mapSlice(module string, n *zyxc.Node, recv *value, elem *types.Type, values bool) *value {
	size, ok := g.arraySize(elem)
	if !ok {
		return nil
	}
	g.declareRuntime("zyxrt_map_len", g.ptrInt(), "ptr")
	count := g.newReg()
	g.emit("  %s = call %s @zyxrt_map_len(ptr %s)", count, g.ptrInt(), recv.text)
	g.declareRuntime("zyxrt_region_alloc_current", "ptr", g.ptrInt(), g.ptrInt())
	buffer := g.newReg()
	g.emit("  %s = call ptr @zyxrt_region_alloc_current(%s %d, %s 8)",
		buffer, g.ptrInt(), size, g.ptrInt())
	fn := "zyxrt_map_keys"
	if values {
		fn = "zyxrt_map_values"
	}
	g.declareRuntime(fn, "void", "ptr", "ptr")
	g.emit("  call void @%s(ptr %s, ptr %s)", fn, recv.text, buffer)
	typ := types.SliceOf(elem)
	first := g.newReg()
	g.emit("  %s = insertvalue %s undef, ptr %s, 0", first, g.llType(typ), buffer)
	out := g.newReg()
	g.emit("  %s = insertvalue %s %s, %s %s, 1", out, g.llType(typ), first, g.ptrInt(), count)
	return &value{text: out, ll: g.llType(typ), typ: typ}
}

// mapKeyList lowers KeyList, which yields an Array of the keys.
func (g *generator) mapKeyList(module string, n *zyxc.Node, recv *value, key *types.Type) *value {
	size, ok := g.arraySize(key)
	if !ok {
		return nil
	}
	typ := types.Array.Instantiate(key)
	g.declareRuntime("zyxrt_map_key_list", "ptr", "ptr", g.ptrInt())
	list := g.newReg()
	g.emit("  %s = call ptr @zyxrt_map_key_list(ptr %s, %s %d)", list, recv.text, g.ptrInt(), size)
	return &value{text: list, ll: "ptr", typ: typ}
}

// setCall lowers one operation on a Set.
func (g *generator) setCall(module string, n *zyxc.Node, recv *value, name string) *value {
	elem := arg(recv.typ, 0)
	switch name {
	case "Len":
		g.declareRuntime("zyxrt_set_len", g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @zyxrt_set_len(ptr %s)", reg, g.ptrInt(), recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Add":
		slot, ok := g.elementSlot(module, n, elem)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_set_add", "void", "ptr", "ptr")
		g.emit("  call void @zyxrt_set_add(ptr %s, ptr %s)", recv.text, slot)
		return &value{ll: "void"}
	case "Contains":
		slot, ok := g.elementSlot(module, n, elem)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_set_contains", "i32", "ptr", "ptr")
		reg := g.newReg()
		g.emit("  %s = call i32 @zyxrt_set_contains(ptr %s, ptr %s)", reg, recv.text, slot)
		return g.boolFrom(reg)
	case "Remove":
		slot, ok := g.elementSlot(module, n, elem)
		if !ok {
			return nil
		}
		out := g.alloca(elem)
		g.declareRuntime("zyxrt_set_remove", "i32", "ptr", "ptr", "ptr")
		flag := g.newReg()
		g.emit("  %s = call i32 @zyxrt_set_remove(ptr %s, ptr %s, ptr %s)", flag, recv.text, slot, out)
		return g.optionValue(module, n, out, elem, flag)
	}
	g.fail(fmt.Errorf("llvmgen: Set.%s is not lowered", name))
	return nil
}

// setLit lowers a set literal: an empty set, then one element at a time.
func (g *generator) setLit(module string, n *zyxc.Node) *value {
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil || typ.Kind != types.KindContainer {
		g.fail(fmt.Errorf("llvmgen: the type of the set literal is not settled"))
		return nil
	}
	empty := &zyxc.Node{Kind: "new"}
	empty.Child(zyxc.New("type", typ.String()))
	set := g.newContainer(module, empty, typ)
	if set == nil {
		return nil
	}
	elem := arg(typ, 0)
	for _, child := range n.Children {
		if child.Kind != "elem" {
			continue
		}
		slot, ok := g.slotOf(module, g.sole(child), elem)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_set_add", "void", "ptr", "ptr")
		g.emit("  call void @zyxrt_set_add(ptr %s, ptr %s)", set.text, slot)
	}
	return set
}

// slotOf evaluates an expression into storage the runtime can read.
func (g *generator) slotOf(module string, e *zyxc.Node, elem *types.Type) (string, bool) {
	v := g.load(g.expr(module, e, elem))
	if v == nil {
		return "", false
	}
	slot := g.alloca(elem)
	g.storeInto(slot, elem, v, false)
	return slot, true
}

// namedSlot evaluates a labelled child into storage the runtime can read.
func (g *generator) namedSlot(module string, n *zyxc.Node, label string, elem *types.Type) (string, bool) {
	v := g.load(g.expr(module, g.wrapped(n, label), elem))
	if v == nil {
		return "", false
	}
	slot := g.alloca(elem)
	g.storeInto(slot, elem, v, false)
	return slot, true
}

// mapHash returns the hash and the comparison the runtime uses for a key. A
// string key is a fat pointer, so it hashes and compares the bytes it names;
// every other key the language knows is compared by its own bytes. A key of a
// declared type needs its own Hash and Eq, which is not lowered.
func (g *generator) mapHash(key *types.Type) (string, string) {
	if key == nil {
		g.fail(fmt.Errorf("llvmgen: a map needs a key type"))
		return "", ""
	}
	if key.Kind == types.KindStr {
		g.declareRuntime("zyxrt_hash_str_key", g.ptrInt(), "ptr", g.ptrInt())
		g.declareRuntime("zyxrt_eq_str_key", "i32", "ptr", "ptr", g.ptrInt())
		return "@zyxrt_hash_str_key", "@zyxrt_eq_str_key"
	}
	switch key.Kind {
	case types.KindBool, types.KindInt8, types.KindInt16, types.KindInt32,
		types.KindInt64, types.KindUint8, types.KindUint16, types.KindUint32,
		types.KindUint64, types.KindUsize, types.KindChar, types.KindRaw,
		types.KindDuration:
		g.declareRuntime("zyxrt_hash_bytes", g.ptrInt(), "ptr", g.ptrInt())
		g.declareRuntime("zyxrt_eq_bytes", "i32", "ptr", "ptr", g.ptrInt())
		return "@zyxrt_hash_bytes", "@zyxrt_eq_bytes"
	}
	g.fail(fmt.Errorf("llvmgen: a %s key needs its own Hash and Eq, which is not lowered", key))
	return "", ""
}

// mapLit lowers a map literal: an empty map, then one entry at a time.
func (g *generator) mapLit(module string, n *zyxc.Node) *value {
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil || typ.Kind != types.KindContainer {
		g.fail(fmt.Errorf("llvmgen: the type of the map literal is not settled"))
		return nil
	}
	empty := &zyxc.Node{Kind: "new"}
	empty.Child(zyxc.New("type", typ.String()))
	m := g.newContainer(module, empty, typ)
	if m == nil {
		return nil
	}
	key, val := arg(typ, 0), arg(typ, 1)
	for _, child := range n.Children {
		if child.Kind != "entry" {
			continue
		}
		keySlot, ok := g.namedSlot(module, child, "key", key)
		if !ok {
			return nil
		}
		valSlot, ok := g.namedSlot(module, child, "value", val)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_map_set", "void", "ptr", "ptr", "ptr")
		g.emit("  call void @zyxrt_map_set(ptr %s, ptr %s, ptr %s)", m.text, keySlot, valSlot)
	}
	return m
}

// elementSlotAt evaluates one argument of an operation into storage the
// runtime can read.
func (g *generator) elementSlotAt(module string, n *zyxc.Node, elem *types.Type, index int) (string, bool) {
	args := g.argNodes(n)
	if index >= len(args) {
		g.fail(fmt.Errorf("llvmgen: the operation needs %d arguments", index+1))
		return "", false
	}
	v := g.load(g.expr(module, args[index], elem))
	if v == nil {
		return "", false
	}
	slot := g.alloca(elem)
	g.storeInto(slot, elem, v, false)
	return slot, true
}

// elementSlot evaluates the single argument of an operation into storage the
// runtime can read.
func (g *generator) elementSlot(module string, n *zyxc.Node, elem *types.Type) (string, bool) {
	v := g.load(g.expr(module, g.sole(n.First("arg")), elem))
	if v == nil {
		return "", false
	}
	slot := g.alloca(elem)
	g.storeInto(slot, elem, v, false)
	return slot, true
}

// arrayTake lowers an operation that yields an Option of an element. The
// runtime reports whether an element was there, and the object carries either
// the element or nothing.
func (g *generator) arrayTake(module string, n *zyxc.Node, recv *value, elem *types.Type, fn, argName string) *value {
	slot := g.alloca(elem)
	parts := []string{"ptr " + recv.text}
	if argName != "" {
		index := g.load(g.expr(module, g.sole(n.First("arg")), types.Usize))
		if index == nil {
			return nil
		}
		parts = append(parts, index.ll+" "+index.text)
	}
	parts = append(parts, "ptr "+slot)
	g.declareRuntime(fn, "i32", "ptr", g.ptrInt(), "ptr")
	flag := g.newReg()
	g.emit("  %s = call i32 @%s(%s)", flag, fn, join(parts))

	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil || typ.Kind != types.KindEnum || len(typ.Args) == 0 {
		g.fail(fmt.Errorf("llvmgen: the result of %s is not settled", fn))
		return nil
	}
	someIndex, _, ok := g.variantOf(typ, "Some")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant Some", typ))
		return nil
	}
	noneIndex, _, ok := g.variantOf(typ, "None")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant None", typ))
		return nil
	}
	obj := g.allocateIn(n, typ)
	tagType := g.tagType(g.tagSize(typ))
	present := g.newReg()
	g.emit("  %s = icmp ne i32 %s, 0", present, flag)
	tag := g.newReg()
	g.emit("  %s = select i1 %s, %s %d, %s %d", tag, present, tagType, someIndex, tagType, noneIndex)
	g.emit("  store %s %s, ptr %s", tagType, tag, obj)

	some := g.newLabel("take.some")
	end := g.newLabel("take.end")
	g.terminator("  br i1 %s, label %%%s, label %%%s", present, some, end)
	g.label(some)
	element := g.load(&value{text: slot, ll: g.llPass(elem), typ: elem, lvalue: true, inline: true})
	g.storeInto(g.payloadElement(obj, typ, []*types.Type{elem}, 0), elem, element, true)
	g.terminator("  br label %%%s", end)
	g.label(end)
	return &value{text: obj, ll: "ptr", typ: typ}
}

// arg returns the nth type argument of a container type.
func arg(t *types.Type, i int) *types.Type {
	if t == nil || i >= len(t.Args) {
		return nil
	}
	return t.Args[i]
}

// join renders a parameter list.
func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// arraySize returns the byte size of an array's element.
func (g *generator) arraySize(elem *types.Type) (uint64, bool) {
	size, err := layout.Size(elem, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", elem, err))
		return 0, false
	}
	return size, true
}
