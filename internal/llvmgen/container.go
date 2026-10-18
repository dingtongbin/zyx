// Containers: the operations on an opaque handle. An element crosses as the
// address of a value of its type, so the runtime moves bytes and the backend
// decides what those bytes mean.

package llvmgen

import (
	"fmt"
	"strings"

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
	case "PriorityQueue":
		return g.queueCall(module, n, recv, name)
	case "BlockingQueue":
		return g.bqueueCall(module, n, recv, name)
	case "Buf":
		return g.bufCall(module, n, recv, name)
	case "Ring":
		return g.ringCall(module, n, recv, name)
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
	if typ.Name == "Ring" {
		elem := arg(typ, 0)
		size, ok := g.arraySize(elem)
		if !ok {
			return nil
		}
		capacity := g.load(g.expr(module, g.sole(n.First("arg")), types.Usize))
		if capacity == nil {
			return nil
		}
		g.declareRuntime("zyxrt_ring_new", "ptr", g.ptrInt(), g.ptrInt())
		reg := g.newReg()
		g.emit("  %s = call ptr @zyxrt_ring_new(%s %d, %s %s)",
			reg, g.ptrInt(), size, g.ptrInt(), capacity.text)
		return &value{text: reg, ll: "ptr", typ: typ}
	}
	if typ.Name == "Buf" {
		capacity := "0"
		if a := g.sole(n.First("arg")); a != nil {
			v := g.load(g.expr(module, a, types.Usize))
			if v == nil {
				return nil
			}
			capacity = v.text
		}
		g.declareRuntime("zyxrt_buf_new", "ptr", g.ptrInt())
		reg := g.newReg()
		g.emit("  %s = call ptr @zyxrt_buf_new(%s %s)", reg, g.ptrInt(), capacity)
		return &value{text: reg, ll: "ptr", typ: typ}
	}
	if typ.Name == "BlockingQueue" {
		elem := arg(typ, 0)
		size, ok := g.arraySize(elem)
		if !ok {
			return nil
		}
		args := g.argNodes(n)
		if len(args) < 2 {
			g.fail(fmt.Errorf("llvmgen: a blocking queue needs a capacity and a comparison"))
			return nil
		}
		capacity := g.load(g.expr(module, args[0], types.Usize))
		if capacity == nil {
			return nil
		}
		less := g.load(g.expr(module, args[1], nil))
		if less == nil {
			return nil
		}
		thunk := g.lessThunk(elem, less)
		g.declareRuntime("zyxrt_bqueue_new", "ptr", g.ptrInt(), g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call ptr @zyxrt_bqueue_new(%s %d, %s %s, ptr %s)",
			reg, g.ptrInt(), size, g.ptrInt(), capacity.text, thunk)
		return &value{text: reg, ll: "ptr", typ: typ}
	}
	if typ.Name == "PriorityQueue" {
		elem := arg(typ, 0)
		size, ok := g.arraySize(elem)
		if !ok {
			return nil
		}
		less := g.load(g.expr(module, g.sole(n.First("arg")), nil))
		if less == nil {
			return nil
		}
		thunk := g.lessThunk(elem, less)
		g.declareRuntime("zyxrt_queue_new", "ptr", g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call ptr @zyxrt_queue_new(%s %d, ptr %s)", reg, g.ptrInt(), size, thunk)
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
		return &value{text: g.arrayLen(g.arrayHeaderOf(recv.text)), ll: g.ptrInt(), typ: types.Usize}
	case "Cap":
		g.declareRuntime("zyxrt_array_cap", g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @zyxrt_array_cap(ptr %s)", reg, g.ptrInt(), recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Clear":
		g.declareRuntime("zyxrt_array_clear", "void", "ptr")
		g.emit("  call void @zyxrt_array_clear(ptr %s)", recv.text)
		return &value{ll: "void"}
	case "Reserve":
		count := g.load(g.expr(module, g.sole(n.First("arg")), types.Usize))
		if count == nil {
			return nil
		}
		g.declareRuntime("zyxrt_array_reserve", "void", "ptr", g.ptrInt())
		g.emit("  call void @zyxrt_array_reserve(ptr %s, %s %s)", recv.text, count.ll, count.text)
		return &value{ll: "void"}
	case "Insert":
		args := g.argNodes(n)
		if len(args) < 2 {
			g.fail(fmt.Errorf("llvmgen: Array.Insert needs an index and a value"))
			return nil
		}
		index := g.load(g.expr(module, args[0], types.Usize))
		slot, ok := g.elementSlotAt(module, n, elem, 1)
		if index == nil || !ok {
			return nil
		}
		g.declareRuntime("zyxrt_array_insert", "void", "ptr", g.ptrInt(), "ptr")
		g.emit("  call void @zyxrt_array_insert(ptr %s, %s %s, ptr %s)", recv.text, index.ll, index.text, slot)
		return &value{ll: "void"}
	case "RemoveRange":
		args := g.argNodes(n)
		if len(args) < 2 {
			g.fail(fmt.Errorf("llvmgen: Array.RemoveRange needs an index and a count"))
			return nil
		}
		index := g.load(g.expr(module, args[0], types.Usize))
		count := g.load(g.expr(module, args[1], types.Usize))
		if index == nil || count == nil {
			return nil
		}
		g.declareRuntime("zyxrt_array_remove_range", "void", "ptr", g.ptrInt(), g.ptrInt())
		g.emit("  call void @zyxrt_array_remove_range(ptr %s, %s %s, %s %s)",
			recv.text, index.ll, index.text, count.ll, count.text)
		return &value{ll: "void"}
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
		slot := g.objectAlloca(elem)
		g.declareRuntime("zyxrt_array_get_unchecked", "void", "ptr", g.ptrInt(), "ptr")
		g.emit("  call void @zyxrt_array_get_unchecked(ptr %s, %s %s, ptr %s)",
			recv.text, index.ll, index.text, slot)
		return g.load(g.inlineObject(slot, elem))
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
	case "Insert":
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
	case "Clear":
		g.declareRuntime("zyxrt_map_clear", "void", "ptr")
		g.emit("  call void @zyxrt_map_clear(ptr %s)", recv.text)
		return &value{ll: "void"}
	}
	g.fail(fmt.Errorf("llvmgen: Map.%s is not lowered", name))
	return nil
}

// A mapPlace is a map entry that is being read or written: the map and the key
// are evaluated once, so a compound assignment reaches the same entry twice
// without evaluating anything twice.
type mapPlace struct {
	recv string
	key  string
	typ  *types.Type
}

// mapPlaceOf evaluates the map and the key of an index node.
func (g *generator) mapPlaceOf(module string, n *zyxc.Node, typ *types.Type) (mapPlace, bool) {
	recv := g.load(g.expr(module, g.wrapped(n, "recv"), nil))
	if recv == nil {
		return mapPlace{}, false
	}
	key, ok := g.namedSlot(module, n, "idx", arg(typ, 0))
	if !ok {
		return mapPlace{}, false
	}
	return mapPlace{recv: recv.text, key: key, typ: typ}, true
}

// mapSlot answers the address of the value an entry holds. The runtime reports
// a key that is not there with a null pointer, which is a fault rather than an
// absent value, because that is what Get is for.
func (g *generator) mapSlot(place mapPlace) string {
	g.declareRuntime("zyxrt_map_slot", "ptr", "ptr", "ptr")
	slot := g.newReg()
	g.emit("  %s = call ptr @zyxrt_map_slot(ptr %s, ptr %s)", slot, place.recv, place.key)
	absent := g.newReg()
	g.emit("  %s = icmp eq ptr %s, null", absent, slot)
	g.guard(absent, "map key not found")
	return slot
}

// mapWrite writes one already-evaluated value into an entry, adding the key
// when it is not there.
func (g *generator) mapWrite(place mapPlace, valSlot string) {
	g.declareRuntime("zyxrt_map_set", "void", "ptr", "ptr", "ptr")
	g.emit("  call void @zyxrt_map_set(ptr %s, ptr %s, ptr %s)", place.recv, place.key, valSlot)
}

// mapIndexOf reports whether an index node reads a map, and answers the map's
// type when it does. The type comes from the annotation the front end wrote,
// so nothing has to be evaluated to decide it.
func (g *generator) mapIndexOf(module string, n *zyxc.Node) (*types.Type, bool) {
	if n == nil || n.Kind != "index" {
		return nil, false
	}
	recv := g.wrapped(n, "recv")
	if recv == nil {
		return nil, false
	}
	t := g.typeOfNode(recv.First("type"), module)
	if t == nil || t.Kind != types.KindContainer || t.Name != "Map" {
		return nil, false
	}
	return t, true
}

// mapTake lowers Get and Remove, which yield an Option of the value.
func (g *generator) mapTake(module string, n *zyxc.Node, recv *value, keySlot string, val *types.Type, remove bool) *value {
	slot := g.objectAlloca(val)
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
	element := g.load(g.inlineObject(slot, elem))
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
	case "Clear":
		g.declareRuntime("zyxrt_map_clear", "void", "ptr")
		g.emit("  call void @zyxrt_map_clear(ptr %s)", recv.text)
		return &value{ll: "void"}
	case "ValueList":
		// The elements are the keys of the table the set is built on, so the
		// list is the key list.
		return g.mapKeyList(module, n, recv, elem)
	case "IsSubset", "Union", "Intersect", "Difference":
		other := g.load(g.expr(module, g.sole(n.First("arg")), nil))
		if other == nil {
			return nil
		}
		if name == "IsSubset" {
			g.declareRuntime("zyxrt_set_is_subset", "i32", "ptr", "ptr")
			reg := g.newReg()
			g.emit("  %s = call i32 @zyxrt_set_is_subset(ptr %s, ptr %s)", reg, recv.text, other.text)
			return g.boolFrom(reg)
		}
		fn := "zyxrt_set_union"
		switch name {
		case "Intersect":
			fn = "zyxrt_set_intersect"
		case "Difference":
			fn = "zyxrt_set_difference"
		}
		g.declareRuntime(fn, "void", "ptr", "ptr")
		g.emit("  call void @%s(ptr %s, ptr %s)", fn, recv.text, other.text)
		return &value{ll: "void"}
	}
	g.fail(fmt.Errorf("llvmgen: Set.%s is not lowered", name))
	return nil
}

// queueCall lowers one operation on a PriorityQueue.
func (g *generator) queueCall(module string, n *zyxc.Node, recv *value, name string) *value {
	elem := arg(recv.typ, 0)
	switch name {
	case "Len":
		g.declareRuntime("zyxrt_queue_len", g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @zyxrt_queue_len(ptr %s)", reg, g.ptrInt(), recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Push":
		slot, ok := g.elementSlot(module, n, elem)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_queue_push", "void", "ptr", "ptr")
		g.emit("  call void @zyxrt_queue_push(ptr %s, ptr %s)", recv.text, slot)
		return &value{ll: "void"}
	case "Pop":
		slot := g.objectAlloca(elem)
		g.declareRuntime("zyxrt_queue_pop", "i32", "ptr", "ptr")
		flag := g.newReg()
		g.emit("  %s = call i32 @zyxrt_queue_pop(ptr %s, ptr %s)", flag, recv.text, slot)
		return g.optionValue(module, n, slot, elem, flag)
	case "Peek":
		slot := g.objectAlloca(elem)
		g.declareRuntime("zyxrt_queue_peek", "i32", "ptr", "ptr")
		flag := g.newReg()
		g.emit("  %s = call i32 @zyxrt_queue_peek(ptr %s, ptr %s)", flag, recv.text, slot)
		return g.optionValue(module, n, slot, elem, flag)
	case "Clear":
		g.declareRuntime("zyxrt_queue_clear", "void", "ptr")
		g.emit("  call void @zyxrt_queue_clear(ptr %s)", recv.text)
		return &value{ll: "void"}
	}
	g.fail(fmt.Errorf("llvmgen: PriorityQueue.%s is not lowered", name))
	return nil
}

// ringCall lowers one operation on a Ring.
func (g *generator) ringCall(module string, n *zyxc.Node, recv *value, name string) *value {
	elem := arg(recv.typ, 0)
	switch name {
	case "Len", "Cap":
		fn := "zyxrt_ring_len"
		if name == "Cap" {
			fn = "zyxrt_ring_cap"
		}
		g.declareRuntime(fn, g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @%s(ptr %s)", reg, g.ptrInt(), fn, recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Push":
		slot, ok := g.elementSlot(module, n, elem)
		if !ok {
			return nil
		}
		g.declareRuntime("zyxrt_ring_push", "void", "ptr", "ptr")
		g.emit("  call void @zyxrt_ring_push(ptr %s, ptr %s)", recv.text, slot)
		return &value{ll: "void"}
	case "Pop":
		slot := g.objectAlloca(elem)
		g.declareRuntime("zyxrt_ring_pop", "i32", "ptr", "ptr")
		flag := g.newReg()
		g.emit("  %s = call i32 @zyxrt_ring_pop(ptr %s, ptr %s)", flag, recv.text, slot)
		return g.optionValue(module, n, slot, elem, flag)
	case "Peek":
		slot := g.objectAlloca(elem)
		g.declareRuntime("zyxrt_ring_peek", "i32", "ptr", "ptr")
		flag := g.newReg()
		g.emit("  %s = call i32 @zyxrt_ring_peek(ptr %s, ptr %s)", flag, recv.text, slot)
		return g.optionValue(module, n, slot, elem, flag)
	case "Clear":
		g.declareRuntime("zyxrt_ring_clear", "void", "ptr")
		g.emit("  call void @zyxrt_ring_clear(ptr %s)", recv.text)
		return &value{ll: "void"}
	}
	g.fail(fmt.Errorf("llvmgen: Ring.%s is not lowered", name))
	return nil
}

// bufCall lowers one operation on a Buf. Bytes answers with the storage in
// place, so the caller reads the buffer rather than a copy of it; Str copies,
// because the text outlives the buffer it was read from.
func (g *generator) bufCall(module string, n *zyxc.Node, recv *value, name string) *value {
	switch name {
	case "Len", "Cap":
		fn := "zyxrt_buf_len"
		if name == "Cap" {
			fn = "zyxrt_buf_cap"
		}
		g.declareRuntime(fn, g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @%s(ptr %s)", reg, g.ptrInt(), fn, recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Clear", "Reset":
		g.declareRuntime("zyxrt_buf_clear", "void", "ptr")
		g.emit("  call void @zyxrt_buf_clear(ptr %s)", recv.text)
		return &value{ll: "void"}
	case "Reserve", "Consume":
		count := g.load(g.expr(module, g.sole(n.First("arg")), types.Usize))
		if count == nil {
			return nil
		}
		fn := "zyxrt_buf_reserve"
		if name == "Consume" {
			fn = "zyxrt_buf_consume"
		}
		g.declareRuntime(fn, "void", "ptr", g.ptrInt())
		g.emit("  call void @%s(ptr %s, %s %s)", fn, recv.text, count.ll, count.text)
		return &value{ll: "void"}
	case "WriteByte":
		b := g.load(g.expr(module, g.sole(n.First("arg")), types.Uint8))
		if b == nil {
			return nil
		}
		g.declareRuntime("zyxrt_buf_write_byte", "void", "ptr", "i8")
		g.emit("  call void @zyxrt_buf_write_byte(ptr %s, i8 %s)", recv.text, b.text)
		return &value{ll: "void"}
	case "Write", "WriteStr":
		v := g.load(g.expr(module, g.sole(n.First("arg")), nil))
		if v == nil {
			return nil
		}
		ptr, size := g.splitFat(v)
		g.declareRuntime("zyxrt_buf_write", "void", "ptr", "ptr", g.ptrInt())
		g.emit("  call void @zyxrt_buf_write(ptr %s, ptr %s, %s %s)",
			recv.text, ptr, g.ptrInt(), size)
		return &value{ll: "void"}
	case "Bytes", "Str":
		fn := "zyxrt_buf_bytes"
		typ := types.SliceOf(types.Uint8)
		if name == "Str" {
			fn = "zyxrt_buf_str"
			typ = types.Str
		}
		ptrSlot := g.alloca(types.Raw)
		sizeSlot := g.alloca(types.Usize)
		g.declareRuntime(fn, "void", "ptr", "ptr", "ptr")
		g.emit("  call void @%s(ptr %s, ptr %s, ptr %s)", fn, recv.text, ptrSlot, sizeSlot)
		ptr := g.newReg()
		g.emit("  %s = load ptr, ptr %s", ptr, ptrSlot)
		size := g.newReg()
		g.emit("  %s = load %s, ptr %s", size, g.ptrInt(), sizeSlot)
		ll := g.llType(typ)
		first := g.newReg()
		g.emit("  %s = insertvalue %s undef, ptr %s, 0", first, ll, ptr)
		out := g.newReg()
		g.emit("  %s = insertvalue %s %s, %s %s, 1", out, ll, first, g.ptrInt(), size)
		return &value{text: out, ll: ll, typ: typ}
	case "Take":
		g.declareRuntime("zyxrt_buf_take", "ptr", "ptr")
		reg := g.newReg()
		g.emit("  %s = call ptr @zyxrt_buf_take(ptr %s)", reg, recv.text)
		return &value{text: reg, ll: "ptr", typ: types.Array.Instantiate(types.Uint8)}
	}
	g.fail(fmt.Errorf("llvmgen: Buf.%s is not lowered", name))
	return nil
}

// bqueueCall lowers one operation on a BlockingQueue. A push that finds the
// queue full and a pop that finds it empty both wait, which the runtime does
// by suspending the running coroutine.
func (g *generator) bqueueCall(module string, n *zyxc.Node, recv *value, name string) *value {
	elem := arg(recv.typ, 0)
	switch name {
	case "Len":
		g.declareRuntime("zyxrt_bqueue_len", g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @zyxrt_bqueue_len(ptr %s)", reg, g.ptrInt(), recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Cap":
		g.declareRuntime("zyxrt_bqueue_cap", g.ptrInt(), "ptr")
		reg := g.newReg()
		g.emit("  %s = call %s @zyxrt_bqueue_cap(ptr %s)", reg, g.ptrInt(), recv.text)
		return &value{text: reg, ll: g.ptrInt(), typ: types.Usize}
	case "Push", "TryPush":
		slot, ok := g.elementSlot(module, n, elem)
		if !ok {
			return nil
		}
		fn := "zyxrt_bqueue_push"
		if name == "TryPush" {
			fn = "zyxrt_bqueue_try_push"
		}
		g.declareRuntime(fn, "i32", "ptr", "ptr")
		reg := g.newReg()
		g.emit("  %s = call i32 @%s(ptr %s, ptr %s)", reg, fn, recv.text, slot)
		if name == "Push" {
			return &value{ll: "void"}
		}
		return g.boolFrom(reg)
	case "Pop":
		slot := g.objectAlloca(elem)
		g.declareRuntime("zyxrt_bqueue_pop", "void", "ptr", "ptr")
		g.emit("  call void @zyxrt_bqueue_pop(ptr %s, ptr %s)", recv.text, slot)
		return g.load(g.inlineObject(slot, elem))
	case "TryPop":
		slot := g.objectAlloca(elem)
		g.declareRuntime("zyxrt_bqueue_try_pop", "i32", "ptr", "ptr")
		flag := g.newReg()
		g.emit("  %s = call i32 @zyxrt_bqueue_try_pop(ptr %s, ptr %s)", flag, recv.text, slot)
		return g.optionValue(module, n, slot, elem, flag)
	}
	g.fail(fmt.Errorf("llvmgen: BlockingQueue.%s is not lowered", name))
	return nil
}

// lessThunk emits the adapter the runtime orders two elements through. The
// comparison the language supplies takes two values of the element type, so
// an element that fits in a register is read out of the heap first and an
// object is passed as the address it already is.
func (g *generator) lessThunk(elem *types.Type, less *value) string {
	// The runtime compares two elements with no receiver, so the thunk
	// reaches a function the compiler named rather than a value it would
	// have to carry into the runtime.
	if less.sym == "" {
		g.fail(fmt.Errorf("llvmgen: a runtime comparison needs a named function, not a method value"))
		return ""
	}
	g.thunks++
	name := fmt.Sprintf("zyx.less.%d", g.thunks)
	saved := g.fn
	g.fn = &funcState{symbol: name, vars: map[string]*varSlot{}}
	g.fn.entry = g.newLabel("less")
	var parts []string
	for _, label := range []string{"%a", "%b"} {
		text := label
		if !byAddress(elem) {
			reg := g.newReg()
			g.emit("  %s = load %s, ptr %s", reg, g.llPass(elem), label)
			text = reg
		}
		parts = append(parts, g.llPass(elem)+" "+text)
	}
	reg := g.newReg()
	// The thunk that drops the receiver is called with a null receiver: the
	// runtime comparison has none.
	g.emit("  %s = call i8 %s(ptr null, %s)", reg, less.sym, strings.Join(parts, ", "))
	wide := g.newReg()
	g.emit("  %s = zext i8 %s to i32", wide, reg)
	g.terminator("  ret i32 %s", wide)
	g.emitThunk(name, "i32", "ptr %a, ptr %b")
	g.fn = saved
	return "@" + name
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
	// The runtime reads an element as the bytes of its type, so an object of
	// a reference type lies in the slot rather than a pointer to it.
	slot := g.objectAlloca(elem)
	g.storeInto(slot, elem, v, true)
	return slot, true
}

// objectAlloca reserves storage for an object itself, which is the shape the
// runtime reads when an element crosses as the bytes of its type.
func (g *generator) objectAlloca(typ *types.Type) string {
	reg := g.newReg()
	fmt.Fprintf(&g.fn.allocas, "  %s = alloca %s\n", reg, g.llType(typ))
	g.countFrame(typ)
	return reg
}

// namedSlot evaluates a labelled child into storage the runtime can read.
func (g *generator) namedSlot(module string, n *zyxc.Node, label string, elem *types.Type) (string, bool) {
	v := g.load(g.expr(module, g.wrapped(n, label), elem))
	if v == nil {
		return "", false
	}
	// The runtime reads an element as the bytes of its type, so an object of
	// a reference type lies in the slot rather than a pointer to it.
	slot := g.objectAlloca(elem)
	g.storeInto(slot, elem, v, true)
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
		types.KindInt64, types.KindInt128, types.KindUint8, types.KindUint16,
		types.KindUint32, types.KindUint64, types.KindUint128, types.KindUsize,
		types.KindChar, types.KindRaw, types.KindDuration:
		g.declareRuntime("zyxrt_hash_bytes", g.ptrInt(), "ptr", g.ptrInt())
		g.declareRuntime("zyxrt_eq_bytes", "i32", "ptr", "ptr", g.ptrInt())
		return "@zyxrt_hash_bytes", "@zyxrt_eq_bytes"
	}
	// A declared type supplies its own Hash and Eq, which the runtime reaches
	// through a thunk of the signature it expects.
	hash, eq, ok := g.userHashFns(key)
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no Hash and Eq, so it cannot be a key", key))
		return "", ""
	}
	return hash, eq
}

// userHashFns returns the hash and the comparison of a key of a declared
// type, which the type supplies through its Hash and Eq.
func (g *generator) userHashFns(key *types.Type) (string, string, bool) {
	hashSym := g.implMethod(key, types.Hash, "Hash")
	eqSym := g.implMethod(key, types.Eq, "Eq")
	if hashSym == "" || eqSym == "" {
		return "", "", false
	}
	return g.hashThunk(hashSym), g.eqThunk(eqSym), true
}

// hashThunk emits the adapter the runtime hashes a declared key through.
func (g *generator) hashThunk(sym string) string {
	g.thunks++
	name := fmt.Sprintf("zyx.hash.%d", g.thunks)
	saved := g.fn
	g.fn = &funcState{symbol: name, vars: map[string]*varSlot{}}
	g.fn.entry = g.newLabel("hash")
	reg := g.newReg()
	g.emit("  %s = call %s @%s(ptr %%key)", reg, g.ptrInt(), llvmName(sym))
	g.terminator("  ret %s %s", g.ptrInt(), reg)
	g.emitThunk(name, g.ptrInt(), "ptr %key, "+g.ptrInt()+" %size")
	g.fn = saved
	return "@" + name
}

// eqThunk emits the adapter the runtime compares a declared key through.
func (g *generator) eqThunk(sym string) string {
	g.thunks++
	name := fmt.Sprintf("zyx.eq.%d", g.thunks)
	saved := g.fn
	g.fn = &funcState{symbol: name, vars: map[string]*varSlot{}}
	g.fn.entry = g.newLabel("eq")
	reg := g.newReg()
	g.emit("  %s = call i8 @%s(ptr %%a, ptr %%b)", reg, llvmName(sym))
	wide := g.newReg()
	g.emit("  %s = zext i8 %s to i32", wide, reg)
	g.terminator("  ret i32 %s", wide)
	g.emitThunk(name, "i32", "ptr %a, ptr %b, "+g.ptrInt()+" %size")
	g.fn = saved
	return "@" + name
}

// emitThunk writes the function the current state accumulated.
func (g *generator) emitThunk(name, ret, params string) {
	var out strings.Builder
	fmt.Fprintf(&out, "define internal %s @%s(%s) {\n", ret, name, params)
	out.WriteString(g.fn.entry + ":\n")
	out.WriteString(g.fn.allocas.String())
	out.WriteString(g.fn.body.String())
	out.WriteString("}\n")
	g.m.funcs.WriteString(out.String())
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
	// The runtime reads an element as the bytes of its type, so an object of
	// a reference type lies in the slot rather than a pointer to it.
	slot := g.objectAlloca(elem)
	g.storeInto(slot, elem, v, true)
	return slot, true
}

// elementSlot evaluates the single argument of an operation into storage the
// runtime can read.
func (g *generator) elementSlot(module string, n *zyxc.Node, elem *types.Type) (string, bool) {
	v := g.load(g.expr(module, g.sole(n.First("arg")), elem))
	if v == nil {
		return "", false
	}
	// The runtime reads an element as the bytes of its type, so an object of
	// a reference type lies in the slot rather than a pointer to it.
	slot := g.objectAlloca(elem)
	g.storeInto(slot, elem, v, true)
	return slot, true
}

// arrayTake lowers an operation that yields an Option of an element. The
// runtime reports whether an element was there, and the object carries either
// the element or nothing.
func (g *generator) arrayTake(module string, n *zyxc.Node, recv *value, elem *types.Type, fn, argName string) *value {
	slot := g.objectAlloca(elem)
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
	element := g.load(g.inlineObject(slot, elem))
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

// The header the runtime writes beside an array, which is what an element access
// reads rather than calling the runtime: the length of the array first, then the
// capacity, the size of one element, the count of the structural modifications,
// and the address of the elements. Both sides hold to this layout, and both say
// so where it is declared.
const arrayHeaderFields = 5

// arrayHeaderOf answers the address an array's length and its elements are read
// through. A container that was never created holds a null handle, and the length
// of one is zero, which is what the runtime answers for it: a null handle reads
// through a zeroed header instead, so an access to one panics where it did rather
// than faulting on the handle.
func (g *generator) arrayHeaderOf(handle string) string {
	none := g.newReg()
	g.emit("  %s = icmp eq ptr %s, null", none, handle)
	header := g.newReg()
	g.emit("  %s = select i1 %s, ptr %s, ptr %s", header, none, g.arrayEmptyGlobal(), handle)
	return header
}

// arrayLen reads the length of an array from its header.
func (g *generator) arrayLen(header string) string {
	length := g.newReg()
	g.emit("  %s = load %s, ptr %s", length, g.ptrInt(), header)
	return length
}

// arrayData reads the address of the elements of an array from its header.
func (g *generator) arrayData(header string) string {
	field := g.newReg()
	g.emit("  %s = getelementptr inbounds i8, ptr %s, i64 %d", field, header,
		uint64(arrayHeaderFields-1)*g.target.Layout().PtrSize)
	data := g.newReg()
	g.emit("  %s = load ptr, ptr %s", data, field)
	return data
}

// arrayEmptyGlobal returns the header a null container is read through,
// declaring it on first use. It is all zeros, and its first field is the length
// of zero that a null container has.
func (g *generator) arrayEmptyGlobal() string {
	if name := g.m.arrayEmpty; name != "" {
		return name
	}
	size := uint64(arrayHeaderFields) * g.target.Layout().PtrSize
	g.m.arrayEmpty = "@.array.empty"
	g.m.globals = append(g.m.globals, fmt.Sprintf(
		"%s = private unnamed_addr constant [%d x i8] zeroinitializer\n", g.m.arrayEmpty, size))
	return g.m.arrayEmpty
}
