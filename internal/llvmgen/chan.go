// Channels: creating one, the five operations on it, and select. A channel is
// an opaque runtime handle, so every operation is a call into the runtime.

package llvmgen

import (
	"fmt"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// newExpr lowers `new T(args)`: a channel is created by the runtime, and a
// struct is built through its init.
func (g *generator) newExpr(module string, n *zyxc.Node) *value {
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of new is not settled"))
		return nil
	}
	switch typ.Kind {
	case types.KindChan:
		return g.newChan(module, n, typ)
	case types.KindContainer:
		return g.newContainer(module, n, typ)
	case types.KindEnum:
		// A construction that can fail answers a Result, so what is built is the
		// value the Result carries and the init call answers the Result itself.
		if inner, ok := constructionOf(typ); ok {
			return g.newStruct(module, n, inner)
		}
	case types.KindStruct, types.KindExternStruct:
		return g.newStruct(module, n, typ)
	}
	g.fail(fmt.Errorf("llvmgen: new %s is not lowered", typ))
	return nil
}

// constructionOf answers the type a construction that can fail builds, which is
// the success type of the Result its init declares.
func constructionOf(t *types.Type) (*types.Type, bool) {
	if t == nil || t.Kind != types.KindEnum || t.Name != "Result" || len(t.Args) != 2 {
		return nil, false
	}
	return t.Args[0], true
}

// newChan creates a channel whose elements are the given size. A missing
// capacity builds a rendezvous channel.
func (g *generator) newChan(module string, n *zyxc.Node, typ *types.Type) *value {
	size, err := layout.Size(typ.Elem, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", typ.Elem, err))
		return nil
	}
	capacity := "0"
	if arg := g.sole(n.First("arg")); arg != nil {
		v := g.load(g.expr(module, arg, types.Usize))
		if v == nil {
			return nil
		}
		capacity = v.text
	}
	g.declareRuntime("zyxrt_chan_new", "ptr", g.ptrInt(), g.ptrInt())
	reg := g.newReg()
	g.emit("  %s = call ptr @zyxrt_chan_new(%s %d, %s %s)", reg, g.ptrInt(), size, g.ptrInt(), capacity)
	return &value{text: reg, ll: "ptr", typ: typ}
}

// newStruct builds a struct through the init of its type.
func (g *generator) newStruct(module string, n *zyxc.Node, typ *types.Type) *value {
	decl := g.tab.decl(typ.ID)
	if decl == nil {
		g.fail(fmt.Errorf("llvmgen: %s has no declaration", typ))
		return nil
	}
	for _, child := range decl.Children {
		if child.Kind != "func" || len(child.Payload) < 3 || child.Payload[0] != "init" {
			continue
		}
		return g.directCall(module, n, child.Payload[2], nil)
	}
	g.fail(fmt.Errorf("llvmgen: %s has no init", typ))
	return nil
}

// chanCall lowers one operation on a channel.
func (g *generator) chanCall(module string, n *zyxc.Node, ch *value, name string) *value {
	elem := ch.typ.Elem
	if !g.crossesAsRegister(elem) {
		g.fail(fmt.Errorf("llvmgen: a channel of %s needs a boundary copy, which is not lowered", elem))
		return nil
	}
	switch name {
	case "Send", "TrySend":
		arg := g.load(g.expr(module, g.sole(n.First("arg")), elem))
		if arg == nil {
			return nil
		}
		slot := g.alloca(elem)
		g.storeInto(slot, elem, arg, false)
		if name == "Send" {
			g.declareRuntime("zyxrt_chan_send", "void", "ptr", "ptr")
			g.emit("  call void @zyxrt_chan_send(ptr %s, ptr %s)", ch.text, slot)
			return &value{ll: "void"}
		}
		g.declareRuntime("zyxrt_chan_try_send", "i32", "ptr", "ptr")
		reg := g.newReg()
		g.emit("  %s = call i32 @zyxrt_chan_try_send(ptr %s, ptr %s)", reg, ch.text, slot)
		return g.boolFrom(reg)
	case "Recv", "TryRecv":
		slot := g.alloca(elem)
		fn := "zyxrt_chan_recv"
		if name == "TryRecv" {
			fn = "zyxrt_chan_try_recv"
		}
		g.declareRuntime(fn, "i32", "ptr", "ptr")
		reg := g.newReg()
		g.emit("  %s = call i32 @%s(ptr %s, ptr %s)", reg, fn, ch.text, slot)
		return g.recvPair(module, n, slot, elem, reg)
	case "Close":
		g.declareRuntime("zyxrt_chan_close", "void", "ptr")
		g.emit("  call void @zyxrt_chan_close(ptr %s)", ch.text)
		return &value{ll: "void"}
	}
	g.fail(fmt.Errorf("llvmgen: %s is not a channel operation", name))
	return nil
}

// recvPair builds the pair a receive answers with: the element, and whether
// one arrived.
func (g *generator) recvPair(module string, n *zyxc.Node, slot string, elem *types.Type, flag string) *value {
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the result of a receive is not settled"))
		return nil
	}
	element := g.load(g.inlineObject(slot, elem))
	first := g.newReg()
	g.emit("  %s = insertvalue %s undef, %s %s, 0", first, g.llType(typ), g.llPass(elem), element.text)
	// A bool is stored as a byte, so the flag is widened to the width the
	// tuple holds before it is inserted.
	widened := g.newReg()
	g.emit("  %s = zext i1 %s to i8", widened, g.boolFrom(flag).text)
	second := g.newReg()
	g.emit("  %s = insertvalue %s %s, i8 %s, 1", second, g.llType(typ), first, widened)
	return &value{text: second, ll: g.llType(typ), typ: typ}
}

// boolFrom turns a runtime flag into a bool.
func (g *generator) boolFrom(reg string) *value {
	out := g.newReg()
	g.emit("  %s = icmp ne i32 %s, 0", out, reg)
	return &value{text: out, ll: "i1", typ: types.Bool}
}

// selectStmt lowers a select. The arms are checked in the order they are
// written, and the first one that is ready runs, so the arm order is the
// priority. With no arm ready and no default, the coroutine parks on every
// channel it watches until one of them becomes ready or the timeout passes.
func (g *generator) selectStmt(module string, n *zyxc.Node) {
	end := g.newLabel("select.end")
	loop := g.newLabel("select.loop")

	// The channels the arms watch are settled once: the select parks on them
	// as a group, so an arm must not evaluate its channel a second time.
	var handles []*value
	var dirs []string
	deadline, timeoutArm := "", (*zyxc.Node)(nil)
	for _, arm := range n.Children {
		switch arm.Kind {
		case "recv-arm", "send-arm":
			ch := g.load(g.expr(module, g.wrapped(arm, "chan"), nil))
			if ch == nil {
				return
			}
			handles = append(handles, ch)
			dir := "0"
			if arm.Kind == "recv-arm" {
				dir = "1"
			}
			dirs = append(dirs, dir)
		case "timeout-arm":
			timeoutArm = arm
			deadline = g.selectDeadline(module, arm)
		}
	}
	if timeoutArm != nil && deadline == "" {
		return
	}

	g.terminator("  br label %%%s", loop)
	g.label(loop)
	at := 0
	for _, arm := range n.Children {
		switch arm.Kind {
		case "recv-arm":
			g.selectRecv(module, arm, handles[at], end)
			at++
		case "send-arm":
			g.selectSend(module, arm, handles[at], end)
			at++
		case "timeout-arm":
			g.selectTimeout(module, arm, deadline, end)
		case "default-arm":
			g.stmt(module, g.wrapped(arm, "body"))
			if !g.fn.terminated {
				g.terminator("  br label %%%s", end)
			}
		}
	}
	if !g.fn.terminated {
		if len(handles) == 0 {
			// A select with no channel arm waits for its deadline, or lets
			// another coroutine run when it has none.
			if timeoutArm != nil {
				g.waitDeadline(deadline)
			} else {
				g.yieldOnce()
			}
		} else {
			// The channels are watched as a group, and the deadline is the
			// timeout arm's, so a channel that becomes ready wakes the select
			// before the timeout passes.
			g.parkSelect(handles, dirs, deadline)
		}
		g.terminator("  br label %%%s", loop)
	}
	g.label(end)
}

// parkSelect lowers the wait a select does when no arm is ready: the
// coroutine parks on every channel it watches, so a channel that becomes
// ready wakes it rather than leaving it to spin. A deadline of the empty
// string means the select has no timeout.
func (g *generator) parkSelect(handles []*value, dirs []string, deadline string) {
	ptr := g.ptrInt()
	size := g.target.Layout().PtrSize
	base := g.allocaSlots("ptr", len(handles))
	for i, h := range handles {
		g.emit("  store ptr %s, ptr %s", h.text, g.gepOffset(base, uint64(i)*size))
	}
	dirBase := g.allocaSlots("i32", len(dirs))
	for i, d := range dirs {
		g.emit("  store i32 %s, ptr %s", d, g.gepOffset(dirBase, uint64(i)*4))
	}
	when := "0"
	if deadline != "" {
		when = deadline
	}
	g.declareRuntime("zyxrt_select_wait", "void", "ptr", "ptr", ptr, ptr)
	g.emit("  call void @zyxrt_select_wait(ptr %s, ptr %s, %s %d, %s %s)",
		base, dirBase, ptr, len(handles), ptr, when)
}

// waitDeadline waits for the deadline of a timeout arm rather than spinning.
func (g *generator) waitDeadline(deadline string) {
	g.declareRuntime("zyxrt_now", g.ptrInt())
	g.declareRuntime("zyxrt_sched_sleep", "void", g.ptrInt())
	now := g.newReg()
	g.emit("  %s = call %s @zyxrt_now()", now, g.ptrInt())
	remaining := g.newReg()
	g.emit("  %s = sub %s %s, %s", remaining, g.ptrInt(), deadline, now)
	g.emit("  call void @zyxrt_sched_sleep(%s %s)", g.ptrInt(), remaining)
}

// yieldOnce lets another ready coroutine run, which a select with no channel
// arm does so the arms are re-checked rather than spun on.
func (g *generator) yieldOnce() {
	g.declareRuntime("zyxrt_sched_yield", "void")
	g.emit("  call void @zyxrt_sched_yield()")
}

// selectDeadline returns the absolute deadline a timeout arm waits for.
func (g *generator) selectDeadline(module string, arm *zyxc.Node) string {
	d := g.load(g.expr(module, g.wrapped(arm, "dur"), nil))
	if d == nil {
		return ""
	}
	g.declareRuntime("zyxrt_now", g.ptrInt())
	start := g.newReg()
	g.emit("  %s = call %s @zyxrt_now()", start, g.ptrInt())
	out := g.newReg()
	g.emit("  %s = add %s %s, %s", out, g.ptrInt(), start, d.text)
	return out
}

// selectTimeout lowers the readiness test of a timeout arm: it is ready when
// its deadline has passed.
func (g *generator) selectTimeout(module string, arm *zyxc.Node, deadline, end string) {
	g.declareRuntime("zyxrt_now", g.ptrInt())
	now := g.newReg()
	g.emit("  %s = call %s @zyxrt_now()", now, g.ptrInt())
	body := g.newLabel("select.arm")
	next := g.newLabel("select.next")
	due := g.newReg()
	g.emit("  %s = icmp uge %s %s, %s", due, g.ptrInt(), now, deadline)
	g.terminator("  br i1 %s, label %%%s, label %%%s", due, body, next)
	g.label(body)
	g.stmt(module, g.wrapped(arm, "body"))
	if !g.fn.terminated {
		g.terminator("  br label %%%s", end)
	}
	g.label(next)
}

// selectRecv lowers a receive arm. The readiness test is separate from the
// receive itself, because a closed channel is ready and answers with the zero
// value: taking the value is the arm body's job. The channel is the value the
// select settled once, so the arm does not evaluate it again.
func (g *generator) selectRecv(module string, arm *zyxc.Node, ch *value, end string) {
	elem := ch.typ.Elem
	g.declareRuntime("zyxrt_chan_recv_ready", "i32", "ptr")
	ready := g.newReg()
	g.emit("  %s = call i32 @zyxrt_chan_recv_ready(ptr %s)", ready, ch.text)
	body := g.newLabel("select.arm")
	next := g.newLabel("select.next")
	g.terminator("  br i1 %s, label %%%s, label %%%s", g.boolFrom(ready).text, body, next)
	g.label(body)
	slot := g.alloca(elem)
	g.declareRuntime("zyxrt_chan_recv", "i32", "ptr", "ptr")
	reg := g.newReg()
	g.emit("  %s = call i32 @zyxrt_chan_recv(ptr %s, ptr %s)", reg, ch.text, slot)
	element := g.inlineObject(slot, elem)
	g.bindArmName(arm, 0, element)
	g.bindArmName(arm, 1, g.boolFrom(reg))
	g.stmt(module, g.wrapped(arm, "body"))
	if !g.fn.terminated {
		g.terminator("  br label %%%s", end)
	}
	g.label(next)
}

// selectSend lowers a send arm. A send that cannot proceed is the panic the
// language defines for a closed channel, so the arm is skipped rather than
// taken: the test asks whether the send would block.
func (g *generator) selectSend(module string, arm *zyxc.Node, ch *value, end string) {
	elem := ch.typ.Elem
	arg := g.load(g.expr(module, g.wrapped(arm, "value"), elem))
	if arg == nil {
		return
	}
	slot := g.alloca(elem)
	g.storeInto(slot, elem, arg, false)
	g.declareRuntime("zyxrt_chan_send_ready", "i32", "ptr")
	ready := g.newReg()
	g.emit("  %s = call i32 @zyxrt_chan_send_ready(ptr %s)", ready, ch.text)
	body := g.newLabel("select.arm")
	next := g.newLabel("select.next")
	g.terminator("  br i1 %s, label %%%s, label %%%s", g.boolFrom(ready).text, body, next)
	g.label(body)
	g.declareRuntime("zyxrt_chan_send", "void", "ptr", "ptr")
	g.emit("  call void @zyxrt_chan_send(ptr %s, ptr %s)", ch.text, slot)
	g.stmt(module, g.wrapped(arm, "body"))
	if !g.fn.terminated {
		g.terminator("  br label %%%s", end)
	}
	g.label(next)
}

// bindArmName binds one name an arm introduces, which the arm's payload
// writes as a name and its symbol.
func (g *generator) bindArmName(arm *zyxc.Node, index int, v *value) {
	name, symbol := "", ""
	if len(arm.Payload) > index*2 {
		name = arm.Payload[index*2]
	}
	if len(arm.Payload) > index*2+1 {
		symbol = arm.Payload[index*2+1]
	}
	if name == "_" || name == "" || symbol == "" {
		return
	}
	slot := g.alloca(v.typ)
	g.storeInto(slot, v.typ, v, false)
	g.fn.vars[symbol] = &varSlot{alloca: slot, typ: v.typ}
}
