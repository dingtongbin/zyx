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
	}
	g.fail(fmt.Errorf("llvmgen: %s.%s is not lowered", recv.typ.Name, name))
	return nil
}

// newContainer creates a container. Its buffer belongs to the region of the
// running coroutine, so it is released with the region that made it.
func (g *generator) newContainer(module string, n *zyxc.Node, typ *types.Type) *value {
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
