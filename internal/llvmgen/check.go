// The checks the language inserts.
//
// The meaning of an operator or an index includes a check: the language
// defines arithmetic that panics rather than wraps, a shift whose amount must
// fit the width, and an index that must lie inside its sequence. A check is
// therefore part of the construct rather than an optimization the backend may
// drop, so it is emitted as an explicit branch that survives every
// optimization level. The built-in forms that skip a check, `wrap_add` and
// `get_unchecked`, are lowered on their own paths.

package llvmgen

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"zyx/internal/types"
)

// guard panics when the condition holds. The branch is explicit, so the check
// is visible in the generated code at every optimization level.
func (g *generator) guard(cond, message string) {
	failed := g.newLabel("check.fail")
	passed := g.newLabel("check.ok")
	g.branch("  br i1 %s, label %%%s, label %%%s", cond, failed, passed)
	g.label(failed)
	g.panicMessage(message)
	g.label(passed)
}

// panicMessage reports one message and ends the function, which is what an
// inserted check does when it fails.
func (g *generator) panicMessage(message string) {
	global := g.m.stringGlobal(message)
	ptr := g.newReg()
	g.emit("  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0",
		ptr, len(message), global)
	g.declareRuntime("zyxrt_panic", "void", "ptr", "i64")
	g.emit("  call void @zyxrt_panic(ptr %s, i64 %d)", ptr, len(message))
	g.emit("  unreachable")
	g.fn.terminated = true
}

// preemptCheck emits the check a loop back edge makes. A coroutine that
// computes without blocking would otherwise hold the scheduler thread, so the
// runtime marks one that has had it long enough and this call lets another
// coroutine run. The check is one call and one atomic load, and the language
// keeps it at every optimization level.
func (g *generator) preemptCheck() {
	g.declareRuntime("zyxrt_preempt_check", "void")
	g.emit("  call void @zyxrt_preempt_check()")
}

// checkedArith lowers an integer addition, subtraction, or multiplication
// with the overflow check the language defines. The intrinsic reports the
// result and whether it overflowed, which is what a checked operation costs
// and what an optimizer can prove away when the range allows.
func (g *generator) checkedArith(op string, a, b *value) *value {
	ll := a.ll
	kind := "u"
	if a.typ.IsSigned() {
		kind = "s"
	}
	step := map[string]string{"+": "add", "-": "sub", "*": "mul"}[op]
	name := fmt.Sprintf("llvm.%s%s.with.overflow.%s", kind, step, ll)
	pair := "{" + ll + ", i1}"
	g.declareRuntime(name, pair, ll, ll)
	reg := g.newReg()
	g.emit("  %s = call %s @%s(%s %s, %s %s)", reg, pair, name, ll, a.text, ll, b.text)
	result := g.newReg()
	g.emit("  %s = extractvalue %s %s, 0", result, pair, reg)
	overflowed := g.newReg()
	g.emit("  %s = extractvalue %s %s, 1", overflowed, pair, reg)
	g.guard(overflowed, "integer overflow")
	return &value{text: result, ll: ll, typ: a.typ}
}

// divisionOverflow returns the condition under which a signed division of a by
// b has no result. Only the smallest value divided by minus one does, and the
// operation would be undefined rather than merely wrong, so it is checked
// beside the divisor.
func (g *generator) divisionOverflow(a, b, ll string) string {
	lowest := new(big.Int).Lsh(big.NewInt(1), uint(intWidth(ll)-1))
	lowest.Neg(lowest)
	atLowest := g.newReg()
	g.emit("  %s = icmp eq %s %s, %s", atLowest, ll, a, lowest.String())
	atMinusOne := g.newReg()
	g.emit("  %s = icmp eq %s %s, -1", atMinusOne, ll, b)
	both := g.newReg()
	g.emit("  %s = and i1 %s, %s", both, atLowest, atMinusOne)
	return both
}

// intWidth returns the bit width of an LLVM integer type name.
func intWidth(ll string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(ll, "i"))
	if err != nil {
		return 64
	}
	return n
}

// checkedIndex reports whether an index lies inside a sequence of the given
// length. The comparison is unsigned, so a negative index reads as the large
// value it is rather than passing as a small one.
func (g *generator) checkedIndex(index, length string) string {
	reg := g.newReg()
	g.emit("  %s = icmp uge i64 %s, %s", reg, index, length)
	return reg
}

// guardIndex panics when an index leaves its sequence.
func (g *generator) guardIndex(index, length string) {
	g.guard(g.checkedIndex(index, length), "index out of range")
}

// guardRange panics when a view of a sequence leaves it: the lower bound may
// not pass the upper one, and neither may pass the length. The comparisons are
// unsigned, so a negative bound reads as the large value it is.
func (g *generator) guardRange(lo, hi, length string) {
	backwards := g.newReg()
	g.emit("  %s = icmp ugt i64 %s, %s", backwards, lo, hi)
	past := g.newReg()
	g.emit("  %s = icmp ugt i64 %s, %s", past, hi, length)
	outsides := g.newReg()
	g.emit("  %s = or i1 %s, %s", outsides, backwards, past)
	g.guard(outsides, "index out of range")
}

// guardDivisor panics when a divisor is zero, and when the signed division it
// belongs to has no result.
func (g *generator) guardDivisor(typ *types.Type, a, b, ll string) {
	zero := g.newReg()
	g.emit("  %s = icmp eq %s %s, 0", zero, ll, b)
	g.guard(zero, "division by zero")
	if typ.IsSigned() {
		g.guard(g.divisionOverflow(a, b, ll), "integer overflow")
	}
}

// guardShift panics when a shift amount does not fit the width of the value.
func (g *generator) guardShift(amount, ll string) {
	reg := g.newReg()
	g.emit("  %s = icmp uge %s %s, %d", reg, ll, amount, intWidth(ll))
	g.guard(reg, "shift amount out of range")
}
