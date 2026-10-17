// The callback subset of a `c func` body. A `c func` is called from C, on the
// blocking-pool thread that made the call: it runs with no coroutine and no
// region of its own, so anything that would park the calling coroutine would
// park the pool thread forever, and anything that would allocate a region has
// no region to allocate from. Value types, raw, the sync primitives, and the
// Try forms of the operations below are what is left, and they are what the
// no-coroutine mode of the standard library is written with.

package check

import (
	"strings"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/types"
)

// cFuncNew reports a runtime allocation inside a `c func` body.
func (c *checker) cFuncNew(v *ast.NewExpr) {
	if !c.inCFunc {
		return
	}
	c.errorHelp(diag.CodeCFuncNew, v.Sp,
		"keep the value in the frame, or let C allocate it",
		"a c func body has no region to allocate in")
}

// cFuncLiteral reports a literal that builds a reference type inside a `c func`
// body, and answers the type either way.
func (c *checker) cFuncLiteral(v ast.Expr, t *types.Type) *types.Type {
	if !c.inCFunc || t == nil || t.Kind == types.Invalid || t.IsValue() {
		return t
	}
	c.errorHelp(diag.CodeCFuncLiteral, v.Span(),
		"build a value type, or let C build the object",
		"a c func body has no region to build %s in", t)
	return t
}

// cFuncSelect reports a select inside a `c func` body.
func (c *checker) cFuncSelect(v *ast.SelectStmt) {
	if !c.inCFunc {
		return
	}
	c.errorHelp(diag.CodeCFuncSelect, v.Sp,
		"poll the descriptor or flag the callback was given",
		"a c func body has no coroutine to park in a select")
}

// cFuncMethod reports a call inside a `c func` body that would park the
// coroutine the body does not have. The receiver decides which operation it is,
// and the Try forms are left alone because they answer instead of waiting.
func (c *checker) cFuncMethod(recv *types.Type, name *ast.Ident) {
	if !c.inCFunc || recv == nil {
		return
	}
	switch recv.Kind {
	case types.KindChan:
		switch name.Name {
		case "Send", "Recv":
			c.errorHelp(diag.CodeCFuncChan, name.Sp,
				"use TrySend or TryRecv, which answer instead of waiting",
				"a c func body has no coroutine to park on a channel")
		}
	}
	if isJoinHandle(recv) && name.Name == "Join" {
		c.errorHelp(diag.CodeCFuncJoin, name.Sp,
			"use TryJoin, which answers instead of waiting",
			"a c func body has no coroutine to wait for a handle in")
	}
	if isMutex(recv) && name.Name == "Lock" {
		c.errorHelp(diag.CodeCFuncMutex, name.Sp,
			"use TryLock, which answers instead of waiting",
			"a c func body has no coroutine to park on a mutex")
	}
}

// cFuncIO reports the language's own input or output wait inside a `c func`
// body. The wait is the runtime entry point the standard library parks a
// coroutine on, so a callback that reaches it has nothing to park.
func (c *checker) cFuncIO(sel *ast.SelectorExpr) {
	if !c.inCFunc || sel.Name.Name != "zyxrt_loop_wait" {
		return
	}
	c.errorHelp(diag.CodeCFuncIO, sel.Name.Sp,
		"poll the descriptor, or let C do the input or output",
		"a c func body has no coroutine to park on input or output")
}

// isJoinHandle reports whether a type is the predeclared coroutine handle.
func isJoinHandle(t *types.Type) bool {
	if t == nil {
		return false
	}
	return t.ID == types.JoinHandle.ID || t.Name == "JoinHandle"
}

// isMutex reports whether a type is the standard library's mutex.
func isMutex(t *types.Type) bool {
	if t == nil {
		return false
	}
	if t.Pkg != "sync" && !strings.HasSuffix(t.Pkg, "/sync") {
		return false
	}
	return t.Name == "Mutex"
}
