// Region hosting of spawned coroutines.

package coro

import "zyx/internal/ast"

// Hosted reports whether a coroutine is hosted by an explicit region block.
// The language requires every spawn to sit inside one, so a coroutine that
// is not hosted only appears in a program that already reported an error.
func (c *Coroutine) Hosted() bool { return c != nil && c.Host != nil }

// HostedBy returns the coroutines a region block hosts, in source order.
// The region waits for all of them before it runs its defers and releases
// its objects.
func (i *Info) HostedBy(host *ast.RegionStmt) []*Coroutine {
	var out []*Coroutine
	for _, c := range i.Coroutines {
		if c.Host == host {
			out = append(out, c)
		}
	}
	return out
}
