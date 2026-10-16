package cache

import (
	"os"
	"path/filepath"
	"testing"
)

// put builds and publishes one entry holding a single artifact.
func put(t *testing.T, c *Cache, key, name, body string) {
	t.Helper()
	entry, err := c.New(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry.Out(), name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	entry.Input("source", HashString(body))
	if err := entry.Add(name); err != nil {
		t.Fatal(err)
	}
	if _, _, err := entry.Publish(); err != nil {
		t.Fatal(err)
	}
}

func TestMissThenHit(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := NewKey()
	key.Add("one", "two")
	sum := key.Sum()
	if _, ok := c.Lookup(sum); ok {
		t.Fatal("an empty cache answered a hit")
	}
	put(t, c, sum, "lib.a", "the library")
	out, ok := c.Lookup(sum)
	if !ok {
		t.Fatal("the entry that was just published was not found")
	}
	data, err := os.ReadFile(filepath.Join(out, "lib.a"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "the library" {
		t.Errorf("artifact = %q, want the library", data)
	}
}

func TestChangedArtifactIsAMiss(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sum := NewKey().Sum()
	put(t, c, sum, "lib.a", "the library")
	// A damaged entry is removed and reported as a miss, so a build rebuilds
	// it rather than linking bytes that do not match the record.
	out, _ := c.Lookup(sum)
	if err := os.WriteFile(filepath.Join(out, "lib.a"), []byte("something else"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup(sum); ok {
		t.Fatal("a changed artifact answered a hit")
	}
	if _, ok := c.Lookup(sum); ok {
		t.Fatal("the damaged entry was not removed")
	}
}

func TestMissingArtifactIsAMiss(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sum := NewKey().Sum()
	put(t, c, sum, "lib.a", "the library")
	out, _ := c.Lookup(sum)
	if err := os.Remove(filepath.Join(out, "lib.a")); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup(sum); ok {
		t.Fatal("a missing artifact answered a hit")
	}
}

func TestAnotherBuilderWinsTheKey(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sum := NewKey().Sum()
	put(t, c, sum, "lib.a", "first")
	entry, err := c.New(sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry.Out(), "lib.a"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := entry.Add("lib.a"); err != nil {
		t.Fatal(err)
	}
	_, won, err := entry.Publish()
	if err != nil {
		t.Fatalf("publishing over an existing entry failed: %v", err)
	}
	if won {
		t.Fatal("the second publisher claimed the key")
	}
	out, ok := c.Lookup(sum)
	if !ok {
		t.Fatal("the entry is gone after the second publisher")
	}
	data, err := os.ReadFile(filepath.Join(out, "lib.a"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first" {
		t.Errorf("artifact = %q, want the first entry to stand", data)
	}
}

func TestDiscardLeavesNothing(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := c.New(NewKey().Sum())
	if err != nil {
		t.Fatal(err)
	}
	dir := entry.Out()
	entry.Discard()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the scratch directory of a discarded entry is still there: %v", err)
	}
}

func TestLockIsExclusive(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := NewKey().Sum()
	release, err := c.Lock(key)
	if err != nil {
		t.Fatal(err)
	}
	// A second holder waits, so the lock is taken in another goroutine that
	// the first one releases.
	done := make(chan struct{})
	go func() {
		second, err := c.Lock(key)
		if err != nil {
			t.Error(err)
		}
		if second != nil {
			second()
		}
		close(done)
	}()
	release()
	<-done
}

func TestKeyFollowsItsInputs(t *testing.T) {
	first := NewKey()
	first.Add("compiler", "runtime")
	second := NewKey()
	second.Add("compiler", "runtime")
	if first.Sum() != second.Sum() {
		t.Error("the same inputs gave different keys")
	}
	third := NewKey()
	third.Add("compiler", "other")
	if first.Sum() == third.Sum() {
		t.Error("different inputs gave the same key")
	}
	// The order is part of the key, so a caller that fixes an order gets a
	// stable one.
	fourth := NewKey()
	fourth.Add("runtime", "compiler")
	if first.Sum() == fourth.Sum() {
		t.Error("a different order gave the same key")
	}
}

func TestScratchIsFresh(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Scratch()
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Scratch()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two scratch directories have the same name")
	}
}
