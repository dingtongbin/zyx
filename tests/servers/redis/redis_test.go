// Package redisserver drives std/redis against a server of the machine.
//
// A corpus under tests/e2e cannot skip a directory, and a corpus that needs a
// server would turn the gate red on a machine without one, so the server suite
// stands apart: it dials the server first and skips when nothing answers.
package redisserver

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"zyx/internal/cli"
	"zyx/internal/toolchain"
)

// redisServerAddress answers the address the suite runs against: the one the
// environment names, or the one a server listens on by default.
func redisServerAddress() string {
	if addr := os.Getenv("ZYX_REDIS_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:6379"
}

// redisServerRoot walks up from the working directory to the module root.
func redisServerRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// redisServerCache answers the one cache directory the suite shares. The
// environment names it when a caller wants one run to reuse what another built,
// and otherwise the checkout's own cache holds it, so the runtime and the shared
// artifacts are built once rather than once per run and per suite.
var redisServerCache = sync.OnceValue(func() string {
	if dir := os.Getenv("ZYX_CACHE"); dir != "" {
		return dir
	}
	if dir := checkoutCache(); dir != "" {
		return dir
	}
	fallback, err := os.MkdirTemp("", "zyx-redis-test-cache-")
	if err != nil {
		panic(err)
	}
	return fallback
})

// checkoutCache answers the cache directory of the checkout the suite runs in, or
// an empty name when there is no checkout to hold one.
func checkoutCache() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			shared := filepath.Join(dir, ".zyx-cache")
			if err := os.MkdirAll(shared, 0o755); err != nil {
				return ""
			}
			return shared
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// redisServerEnv gives the compiler the standard library, the runtime tree, and
// the cache of the checkout.
func redisServerEnv(root string) func(string) string {
	return func(name string) string {
		switch name {
		case "ZYX_STD":
			return filepath.Join(root, "std")
		case "ZYX_RUNTIME":
			return filepath.Join(root, "runtime")
		case "ZYX_CACHE":
			return redisServerCache()
		}
		return ""
	}
}

// redisServerRun invokes the command line in a directory and returns its standard
// output, standard error, and exit code.
func redisServerRun(t *testing.T, root, dir string, args ...string) (string, string, int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	s := &cli.Session{Stdout: &out, Stderr: &errBuf, Dir: dir, Env: redisServerEnv(root)}
	code := s.Run(args)
	return out.String(), errBuf.String(), code
}

// redisServerProgram is the program the suite runs. Every key it touches carries
// its own prefix and is deleted before and after, so the output depends on
// nothing but what the program itself wrote: no other program's keys are read,
// and no server error text is printed, because that text belongs to the version
// of the server rather than to this program.
const redisServerProgram = `package main

import "std/fmt"
import "std/os"
import "std/redis"

const PREFIX: str = "zyx_server_suite:"

func args_of(a: str, b: str) -> Array<str> {
    var out = new Array<str>(0)
    out.Push(a)
    out.Push(b)
    return out
}

func one(a: str) -> Array<str> {
    var out = new Array<str>(0)
    out.Push(a)
    return out
}

func none() -> Array<str> {
    return new Array<str>(0)
}

// show names a reply by its shape. An error the server reported is named without
// its text, which belongs to the server rather than to this program.
func show(r: redis.Reply) -> str {
    match r {
        Status(s) => { return f"status {s}" }
        Error(s) => { return "error" }
        Int(n) => { return f"int {n}" }
        Bulk(s) => { return f"bulk {s}" }
        Nil => { return "nil" }
        Array(xs) => {
            var out = new Buf(0)
            out.WriteStr(f"array {i64(xs.Len())}")
            for x in xs {
                out.WriteStr(f" [{show(x)}]")
            }
            return out.Str()
        }
    }
}

func ask(client: redis.Client, label: str, cmd: str, args: Array<str>) {
    match client.Do(cmd, args) {
        Ok(r) => { fmt.Println(f"{label} {show(r)}") }
        Err(e) => { fmt.Println(f"{label} failed {i64(i32(e.Kind))}") }
    }
}

func keys(a: str, b: str) -> Array<str> {
    var out = new Array<str>(0)
    out.Push(f"{PREFIX}{a}")
    out.Push(f"{PREFIX}{b}")
    return out
}

func list_of(a: str, b: str, third: str) -> Array<str> {
    var out = new Array<str>(0)
    out.Push(a)
    out.Push(b)
    out.Push(third)
    return out
}

func run(client: redis.Client) {
    // The keys of this program are the only ones it reads, and they begin from
    // nothing every time it runs: what they held before is not its business, so
    // the first clear is silent and the last two say what they took.
    var owned = list_of(f"{PREFIX}a", f"{PREFIX}b", f"{PREFIX}c")
    drop(client, owned)
    ask(client, "ping", "PING", none())
    ask(client, "set", "SET", args_of(f"{PREFIX}a", "hello"))
    ask(client, "get", "GET", one(f"{PREFIX}a"))
    ask(client, "exists", "EXISTS", one(f"{PREFIX}a"))
    ask(client, "missing", "GET", one(f"{PREFIX}not-there"))
    ask(client, "incr", "INCR", one(f"{PREFIX}b"))
    ask(client, "incr again", "INCR", one(f"{PREFIX}b"))
    ask(client, "type error", "INCR", one(f"{PREFIX}a"))
    ask(client, "arity", "GET", none())
    ask(client, "array", "MGET", keys("a", "b"))
    ask(client, "push", "RPUSH", args_of(f"{PREFIX}c", "one"))
    ask(client, "push again", "RPUSH", args_of(f"{PREFIX}c", "two"))
    ask(client, "range", "LRANGE", list_of(f"{PREFIX}c", "0", "-1"))
    ask(client, "clean", "DEL", owned)
    ask(client, "clean again", "DEL", owned)
    client.Close()
    ask(client, "closed call", "PING", none())
}

// drop gives keys back to the server without printing what it answered.
func drop(client: redis.Client, ks: Array<str>) {
    match client.Do("DEL", ks) {
        Ok(_) => { }
        Err(_) => { }
    }
}

func main() {
    var addr = "127.0.0.1:6379"
    match os.Env("ZYX_REDIS_ADDR") {
        Some(v) => { addr = v }
        None => { }
    }
    match new redis.Client(addr) {
        Ok(client) => { run(client) }
        Err(e) => { fmt.Println("open failed") }
    }
    match new redis.Client("127.0.0.1:not-a-port") {
        Ok(never) => { never.Close() }
        Err(e) => { fmt.Println(f"bad address {i64(i32(e.Kind))}") }
    }
}
`

// redisServerWant is what the program prints when every path of std/redis works
// against the server.
const redisServerWant = `ping status PONG
set status OK
get bulk hello
exists int 1
missing nil
incr int 1
incr again int 2
type error error
arity error
array array 2 [bulk hello] [bulk 2]
push int 1
push again int 2
range array 2 [bulk one] [bulk two]
clean int 3
clean again int 0
closed call failed 6
bad address 5
`

// TestRedisServer builds and runs a program against the redis server the
// environment names and compares its output byte for byte. Nothing answers, the
// suite skips rather than fails, so a machine without a server keeps the gate
// green.
func TestRedisServer(t *testing.T) {
	if testing.Short() {
		t.Skip("the server suite runs in the full gate")
	}
	tc, err := toolchain.Locate()
	if err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	address := redisServerAddress()
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Skipf("no redis server answers at %s: %v", address, err)
	}
	conn.Close()

	// The client library is a shared object on this machine, so the directory of
	// the compiler is put where the program can find it as well.
	if tc.Clang != "" {
		t.Setenv("PATH", filepath.Dir(tc.Clang)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	t.Setenv("ZYX_REDIS_ADDR", address)

	root := redisServerRoot(t)
	dir := t.TempDir()
	write := map[string]string{
		"zyx.mod":  "module example.com/redisserver\n\nzyx 0.1\n",
		"main.zyx": redisServerProgram,
	}
	for name, body := range write {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// No library flags are given: the toolchain knows what the packages the
	// program reaches need.
	stdout, stderr, code := redisServerRun(t, root, dir, "run", "./...")
	if code != 0 {
		t.Fatalf("run failed with %d:\n%s", code, stderr)
	}
	if stdout != redisServerWant {
		t.Errorf("output =\n%s\nwant\n%s", stdout, redisServerWant)
	}
}
