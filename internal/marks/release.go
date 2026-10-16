// Static release insertion for unshared objects.

package marks

// release marks an object that no sharing reaches: the last use is the
// release point, and the object's memory returns to the pool there rather
// than at region exit.
func (b *builder) release(a *allocation) {
	last := b.lastUse[a]
	if last == nil {
		// The object is never used again, so the allocation itself is the
		// release point.
		b.info.Add(a.Node, "last-use", "")
		b.info.Add(a.Node, "static-release", "")
		b.info.Add(a.Node, "why-last-use", "")
		return
	}
	b.info.Add(last, "last-use", "")
	b.info.Add(last, "static-release", "")
	b.info.Add(last, "why-last-use", "")
}
