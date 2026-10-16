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
	case types.KindStruct:
		return g.newStruct(module, n, typ)
	}
	g.fail(fmt.Errorf("llvmgen: new %s is not lowered", typ))
	return nil
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
	element := g.load(&value{text: slot, ll: g.llPass(elem), typ: elem, lvalue: true, inline: true})
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
// priority. With no arm ready and no default, the coroutine yields and tries
// again.
func (g *generator) selectStmt(module string, n *zyxc.Node) {
	end := g.newLabel("select.end")
	loop := g.newLabel("select.loop")

	// A timeout arm bounds the wait, so its deadline is taken once, before
	// the arms are first checked.
	deadline, timeoutArm := "", (*zyxc.Node)(nil)
	for _, arm := range n.Children {
		if arm.Kind == "timeout-arm" {
			timeoutArm = arm
			deadline = g.selectDeadline(module, arm)
		}
	}
	if timeoutArm != nil && deadline == "" {
		return
	}

	g.terminator("  br label %%%s", loop)
	g.label(loop)
	hasDefault := false
	for _, arm := range n.Children {
		switch arm.Kind {
		case "recv-arm":
			g.selectRecv(module, arm, end)
		case "send-arm":
			g.selectSend(module, arm, end)
		case "timeout-arm":
			g.selectTimeout(module, arm, deadline, end)
		case "default-arm":
			hasDefault = true
			g.stmt(module, g.wrapped(arm, "body"))
			if !g.fn.terminated {
				g.terminator("  br label %%%s", end)
			}
		}
	}
	if !hasDefault && timeoutArm == nil && !g.fn.terminated {
		// Nothing bounds the wait and no arm is ready: let another coroutine
		// run and try again.
		g.declareRuntime("zyxrt_sched_yield", "void")
		g.emit("  call void @zyxrt_sched_yield()")
		g.terminator("  br label %%%s", loop)
	} else if timeoutArm != nil && !g.fn.terminated {
		// The deadline has not passed: wait for it rather than spin.
		g.declareRuntime("zyxrt_sched_sleep", "void", g.ptrInt())
		g.declareRuntime("zyxrt_now", g.ptrInt())
		now := g.newReg()
		g.emit("  %s = call %s @zyxrt_now()", now, g.ptrInt())
		remaining := g.newReg()
		g.emit("  %s = sub %s %s, %s", remaining, g.ptrInt(), deadline, now)
		g.emit("  call void @zyxrt_sched_sleep(%s %s)", g.ptrInt(), remaining)
		g.terminator("  br label %%%s", loop)
	}
	g.label(end)
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
// value: taking the value is the arm body's job.
func (g *generator) selectRecv(module string, arm *zyxc.Node, end string) {
	ch := g.load(g.expr(module, g.wrapped(arm, "chan"), nil))
	if ch == nil {
		return
	}
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
	element := &value{text: slot, ll: g.llPass(elem), typ: elem, lvalue: true, inline: true}
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
func (g *generator) selectSend(module string, arm *zyxc.Node, end string) {
	ch := g.load(g.expr(module, g.wrapped(arm, "chan"), nil))
	if ch == nil {
		return
	}
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
