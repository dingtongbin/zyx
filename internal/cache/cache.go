// Package cache is the content-addressed build cache. Every entry is stored
// under the hash of everything that produced it, published atomically, and
// checked when it is read, so a build that finds an entry can use it without
// looking at timestamps or trusting what the directory claims.

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// A Cache is a directory of entries, each named by the key of its inputs.
type Cache struct {
	root string
}

// seq numbers the scratch directories of one process, so two builds running
// at the same time never share one.
var seq atomic.Uint64

// scratchName returns a name no other builder in this process is using.
func scratchName() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), seq.Add(1))
}

// Open prepares a cache directory, creating the layout the entries, the
// scratch directories, and the locks live in.
func Open(root string) (*Cache, error) {
	if root == "" {
		return nil, errors.New("cache: no directory")
	}
	for _, dir := range []string{root, filepath.Join(root, "tmp"), filepath.Join(root, "locks")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("cache: %w", err)
		}
	}
	return &Cache{root: root}, nil
}

// Root returns the directory the cache lives in.
func (c *Cache) Root() string { return c.root }

// Scratch returns a directory the caller may fill and remove. It lives under
// the cache so a build's intermediates stay in one place.
func (c *Cache) Scratch() (string, error) {
	dir := filepath.Join(c.root, "tmp", scratchName())
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// entryDir returns where the entry of a key lives. The leading characters of
// the key spread the entries over directories, so one directory does not grow
// without bound.
func (c *Cache) entryDir(key string) string {
	prefix := key
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	return filepath.Join(c.root, prefix, key)
}

// Lookup answers the directory of a key's artifacts, and whether there is a
// usable entry. An entry whose record does not match the key, or whose
// artifact is missing or changed, is removed and reported as a miss: a
// damaged cache repairs itself rather than feeding a build wrong bytes.
func (c *Cache) Lookup(key string) (string, bool) {
	dir := c.entryDir(key)
	record, err := readRecord(filepath.Join(dir, "meta"))
	if err != nil || record.key != key {
		c.remove(dir)
		return "", false
	}
	out := filepath.Join(dir, "out")
	for _, artifact := range record.artifacts {
		name, want, ok := splitPair(artifact)
		if !ok {
			c.remove(dir)
			return "", false
		}
		got, err := HashFile(filepath.Join(out, name))
		if err != nil || got != want {
			c.remove(dir)
			return "", false
		}
	}
	return out, true
}

// An Entry is one entry being built: a scratch directory the producer fills
// with artifacts, and the record that is published beside them.
type Entry struct {
	cache     *Cache
	key       string
	dir       string
	out       string
	record    record
	published bool
}

// New starts an entry for a key. The caller fills Out and then publishes the
// entry, or discards it and builds without a cache.
func (c *Cache) New(key string) (*Entry, error) {
	dir := filepath.Join(c.root, "tmp", scratchName())
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	return &Entry{
		cache:  c,
		key:    key,
		dir:    dir,
		out:    out,
		record: record{key: key},
	}, nil
}

// Out returns the directory the artifacts go in.
func (e *Entry) Out() string { return e.out }

// Input records one input the entry was built from. The name is relative, so
// the record never carries a host path.
func (e *Entry) Input(name, hash string) {
	e.record.inputs = append(e.record.inputs, name+" "+hash)
}

// Add records an artifact, hashing what the producer wrote.
func (e *Entry) Add(name string) error {
	hash, err := HashFile(filepath.Join(e.out, name))
	if err != nil {
		return err
	}
	e.record.artifacts = append(e.record.artifacts, name+" "+hash)
	return nil
}

// Diagnostic records one diagnostic of the entry, as the command line writes
// it. A front end that reported one produced no artifacts, and the report is
// replayed byte for byte when the entry is read back, which is what makes a
// warm run print what the cold one printed.
func (e *Entry) Diagnostic(text string) {
	e.record.diagnostics = append(e.record.diagnostics, escapeRecord(text))
}

// Diagnostics returns the diagnostics an entry holds, in the order they were
// recorded. An entry that holds none answers nothing.
func (c *Cache) Diagnostics(key string) []string {
	record, err := readRecord(filepath.Join(c.entryDir(key), "meta"))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(record.diagnostics))
	for _, line := range record.diagnostics {
		out = append(out, unescapeRecord(line))
	}
	return out
}

// escapeRecord renders text as one line of a record: the newlines that make a
// diagnostic several lines are written as escapes, so a line stays a line.
func escapeRecord(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		switch c := text[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// unescapeRecord returns the text escapeRecord wrote.
func unescapeRecord(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] != '\\' || i+1 >= len(line) {
			b.WriteByte(line[i])
			continue
		}
		i++
		switch line[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(line[i])
		}
	}
	return b.String()
}

// Publish moves the entry into place and answers the directory its artifacts
// are in, which is the directory Lookup answers with. It reports false when
// another builder published the same key first, which is a hit rather than a
// failure: the directory it answers with is then that builder's.
func (e *Entry) Publish() (string, bool, error) {
	if e.published {
		return "", false, errors.New("cache: the entry was already published")
	}
	e.published = true
	if err := writeRecord(filepath.Join(e.dir, "meta"), e.record); err != nil {
		e.Discard()
		return "", false, err
	}
	dest := e.cache.entryDir(e.key)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		e.Discard()
		return "", false, err
	}
	if err := ReplaceFile(e.dir, dest); err != nil {
		// Another builder reached the key first. Its entry is as good as
		// this one, so this one is dropped.
		e.Discard()
		if _, ok := e.cache.Lookup(e.key); ok {
			return filepath.Join(dest, "out"), false, nil
		}
		return "", false, err
	}
	return filepath.Join(dest, "out"), true, nil
}

// ReplaceFile moves src onto dst, replacing whatever is there. Windows refuses
// a rename while the target is open, and a running program, a scanner, or an
// indexer can hold it for a moment, so the attempt is repeated briefly before
// the error is reported. A retry changes nothing about what is written, so the
// determinism contract is unaffected.
func ReplaceFile(src, dst string) error {
	const attempts = 10
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}

// Discard removes the scratch directory of an entry that was not published.
func (e *Entry) Discard() {
	if e.dir != "" {
		os.RemoveAll(e.dir)
	}
}

// remove deletes an entry directory, which is what a miss that found a
// damaged entry does.
func (c *Cache) remove(dir string) { os.RemoveAll(dir) }

// lockWait bounds how long a builder waits for a key before taking the lock
// over: a lock whose holder is gone must not stop every later build.
const lockWait = 10 * time.Second

// Lock takes the per-key lock, which keeps two builders from compiling the
// same entry at once, and answers the function that releases it.
func (c *Cache) Lock(key string) (func(), error) {
	path := filepath.Join(c.root, "locks", key+".lock")
	deadline := time.Now().Add(lockWait)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(file, "%d\n", os.Getpid())
			file.Close()
			return func() { os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			// The holder is gone, or it is taking too long. The lock only
			// saves work, so it is taken over rather than waited on.
			os.Remove(path)
			deadline = time.Now().Add(lockWait)
			continue
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A Key accumulates the inputs of one entry. Inputs are added in a fixed
// order, so the same inputs give the same key wherever the build runs.
type Key struct {
	parts []string
}

// NewKey returns an empty key.
func NewKey() *Key { return &Key{} }

// Add appends one input, in the order the caller established.
func (k *Key) Add(parts ...string) {
	for _, part := range parts {
		k.parts = append(k.parts, part)
	}
}

// Sum returns the key: the SHA-256 of the inputs, in lowercase hexadecimal.
func (k *Key) Sum() string {
	sum := sha256.Sum256([]byte(strings.Join(k.parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// A record is what an entry says about itself: the key it was built for, the
// inputs that went into it, the artifacts it holds, and the diagnostics it
// reported. It is a text file so a reader can tell what a stale entry was
// built from.
type record struct {
	key         string
	inputs      []string
	artifacts   []string
	diagnostics []string
}

// readRecord reads an entry's record.
func readRecord(path string) (record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return record{}, err
	}
	var r record
	for _, line := range strings.Split(string(data), "\n") {
		field, rest, _ := strings.Cut(line, " ")
		switch field {
		case "key":
			r.key = rest
		case "input":
			r.inputs = append(r.inputs, rest)
		case "artifact":
			r.artifacts = append(r.artifacts, rest)
		case "diagnostic":
			r.diagnostics = append(r.diagnostics, rest)
		}
	}
	if r.key == "" {
		return record{}, errors.New("cache: the record names no key")
	}
	return r, nil
}

// writeRecord writes an entry's record.
func writeRecord(path string, r record) error {
	var b strings.Builder
	fmt.Fprintf(&b, "key %s\n", r.key)
	for _, in := range r.inputs {
		fmt.Fprintf(&b, "input %s\n", in)
	}
	for _, artifact := range r.artifacts {
		fmt.Fprintf(&b, "artifact %s\n", artifact)
	}
	for _, diagnostic := range r.diagnostics {
		fmt.Fprintf(&b, "diagnostic %s\n", diagnostic)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// splitPair splits a recorded "name hash" pair.
func splitPair(s string) (name, value string, ok bool) {
	name, value, ok = strings.Cut(s, " ")
	if !ok || name == "" || value == "" {
		return "", "", false
	}
	return name, value, true
}

// HashFile returns the SHA-256 of a file's contents.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return hashReader(file)
}

// HashBytes returns the SHA-256 of a byte string.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashString returns the SHA-256 of a string.
func HashString(s string) string { return HashBytes([]byte(s)) }

// hashReader returns the SHA-256 of everything a reader yields.
func hashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
