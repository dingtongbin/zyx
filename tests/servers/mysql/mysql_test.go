// Package mysqlserver drives std/mysql against a server of the machine.
//
// A corpus under tests/e2e cannot skip a directory, and a corpus that needs a
// server would turn the gate red on a machine without one, so the server suite
// stands apart: it dials the server first and skips when nothing answers.
package mysqlserver

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"zyx/internal/cli"
	"zyx/internal/toolchain"
)

// mysqlServerDSN answers the data source name the suite runs against: the one
// the environment names, or the local default.
func mysqlServerDSN() string {
	if dsn := os.Getenv("ZYX_MYSQL_DSN"); dsn != "" {
		return dsn
	}
	return "root:123456@tcp(127.0.0.1:3306)/"
}

// mysqlServerAddress answers the host and port a data source name names, which
// is what the suite dials to find out whether a server answers.
func mysqlServerAddress(dsn string) string {
	open := strings.Index(dsn, "tcp(")
	if open < 0 {
		return "127.0.0.1:3306"
	}
	rest := dsn[open+len("tcp("):]
	end := strings.Index(rest, ")")
	if end < 0 {
		return "127.0.0.1:3306"
	}
	if address := rest[:end]; address != "" {
		return address
	}
	return "127.0.0.1:3306"
}

// mysqlServerRoot walks up from the working directory to the module root.
func mysqlServerRoot(t *testing.T) string {
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

// mysqlServerCache answers the one cache directory the suite shares, so the
// runtime is built once rather than for every run.
var mysqlServerCache = sync.OnceValue(func() string {
	dir, err := os.MkdirTemp("", "zyx-mysql-test-cache-")
	if err != nil {
		panic(err)
	}
	return dir
})

// mysqlServerEnv gives the compiler the standard library, the runtime tree,
// and the cache of the checkout.
func mysqlServerEnv(root string) func(string) string {
	return func(name string) string {
		switch name {
		case "ZYX_STD":
			return filepath.Join(root, "std")
		case "ZYX_RUNTIME":
			return filepath.Join(root, "runtime")
		case "ZYX_CACHE":
			return mysqlServerCache()
		}
		return ""
	}
}

// mysqlServerRun invokes the command line in a directory and returns its
// standard output, standard error, and exit code.
func mysqlServerRun(t *testing.T, root, dir string, args ...string) (string, string, int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	s := &cli.Session{Stdout: &out, Stderr: &errBuf, Dir: dir, Env: mysqlServerEnv(root)}
	code := s.Run(args)
	return out.String(), errBuf.String(), code
}

// mysqlServerProgram is the program the suite runs. It makes a database of its
// own, works on a table in it, and prints only what its own rows say, so the
// output does not depend on the server or the account. It reads the data
// source name from the environment, where the suite puts it.
const mysqlServerProgram = `package main

import "std/db"
import "std/fmt"
import "std/mysql"
import "std/os"

const DSN_DEFAULT: str = "root:123456@tcp(127.0.0.1:3306)/"
const DB: str = "zyx_server_suite"

func none() -> Array<Value> {
    return new Array<Value>(0)
}

func five(v1: Value, v2: Value, v3: Value, v4: Value, v5: Value) -> Array<Value> {
    var out = new Array<Value>(0)
    out.Push(v1)
    out.Push(v2)
    out.Push(v3)
    out.Push(v4)
    out.Push(v5)
    return out
}

func one(value: Value) -> Array<Value> {
    var out = new Array<Value>(0)
    out.Push(value)
    return out
}

func pair(first: Value, second: Value) -> Array<Value> {
    var out = new Array<Value>(0)
    out.Push(first)
    out.Push(second)
    return out
}

func kind_name(k: db.ErrorKind) -> str {
    match k {
        NotFound => { return "NotFound" }
        Permission => { return "Permission" }
        Exists => { return "Exists" }
        NotDir => { return "NotDir" }
        IsDir => { return "IsDir" }
        Invalid => { return "Invalid" }
        Closed => { return "Closed" }
        Timeout => { return "Timeout" }
        Interrupted => { return "Interrupted" }
        WouldBlock => { return "WouldBlock" }
        Eof => { return "Eof" }
        AddrInUse => { return "AddrInUse" }
        AddrNotAvailable => { return "AddrNotAvailable" }
        Refused => { return "Refused" }
        Reset => { return "Reset" }
        Unreachable => { return "Unreachable" }
        Other => { return "Other" }
    }
}

func note_of(rows: db.Rows) -> str {
    if rows.Null(3) {
        return "none"
    }
    return rows.Text(3)
}

func setup(conn: db.Conn) -> Result<(), db.Error> {
    try conn.Exec(f"DROP DATABASE IF EXISTS {DB}", none())
    try conn.Exec(f"CREATE DATABASE {DB}", none())
    try conn.Exec(f"CREATE TABLE {DB}.items (id INT PRIMARY KEY, name TEXT, amount DOUBLE, note TEXT, big BIGINT)", none())
    fmt.Println("setup ok")
    var first = five(Value.Num(1.0), Value.Str("Ada"), Value.Num(1.5), Value.Null, Value.Str("10"))
    try conn.Exec(f"INSERT INTO {DB}.items (id, name, amount, note, big) VALUES (?, ?, ?, ?, ?)", first)
    var second = five(Value.Num(2.0), Value.Str("O'Brien"), Value.Num(0.0), Value.Str("zero"), Value.Str("0"))
    try conn.Exec(f"INSERT INTO {DB}.items (id, name, amount, note, big) VALUES (?, ?, ?, ?, ?)", second)
    var third = five(Value.Num(3.0), Value.Str("gamma"), Value.Num(2.25), Value.Str("wide"), Value.Str("9223372036854775807"))
    try conn.Exec(f"INSERT INTO {DB}.items (id, name, amount, note, big) VALUES (?, ?, ?, ?, ?)", third)
    fmt.Println("inserts ok")
    return Result.Ok()
}

func read(conn: db.Conn) -> Result<(), db.Error> {
    var rows = try conn.Query(f"SELECT id, name, amount, note, big FROM {DB}.items ORDER BY id", none())
    var names = rows.Columns()
    fmt.Println(f"columns {i64(names.Len())}")
    for name in names {
        fmt.Println(f"column {name}")
    }
    for rows.Next() {
        var note = note_of(rows)
        fmt.Println(f"row id {rows.Int(0)} name {rows.Text(1)} amount {rows.Float(2)} note {note} big {rows.Int(4)} null {rows.Null(3)}")
    }
    rows.Close()
    return Result.Ok()
}

func prepared(conn: db.Conn) -> Result<(), db.Error> {
    var by_id = try conn.Query(f"SELECT name FROM {DB}.items WHERE id = ?", one(Value.Num(2.0)))
    if by_id.Next() {
        fmt.Println(f"by id {by_id.Text(0)}")
    }
    by_id.Close()

    var by_name = try conn.Query(f"SELECT id FROM {DB}.items WHERE name = ?", one(Value.Str("gamma")))
    if by_name.Next() {
        fmt.Println(f"by name {by_name.Int(0)}")
    }
    by_name.Close()

    var by_null = try conn.Query(f"SELECT id FROM {DB}.items WHERE note <=> ?", one(Value.Null))
    if by_null.Next() {
        fmt.Println(f"by null {by_null.Int(0)}")
    }
    by_null.Close()

    var zero = try conn.Query(f"SELECT amount FROM {DB}.items WHERE id = ?", one(Value.Num(2.0)))
    if zero.Next() {
        fmt.Println(f"zero {zero.Float(0)}")
    }
    zero.Close()

    var wide = try conn.Query(f"SELECT big FROM {DB}.items WHERE id = ?", one(Value.Num(3.0)))
    if wide.Next() {
        fmt.Println(f"wide {wide.Int(0)}")
    }
    wide.Close()

    var text = try conn.Query(f"SELECT name FROM {DB}.items WHERE id = ?", one(Value.Num(2.0)))
    if text.Next() {
        fmt.Println(f"text {text.Text(0)}")
    }
    text.Close()

    var wide_text = try conn.Query("SELECT REPEAT('x', ?)", one(Value.Num(5000.0)))
    if wide_text.Next() {
        fmt.Println(f"wide text {i64(wide_text.Text(0).Len())}")
    }
    wide_text.Close()
    return Result.Ok()
}

func failures(conn: db.Conn) {
    match conn.Exec(f"INSERT INTO {DB}.items (id, name) VALUES (?, ?)", pair(Value.Num(1.0), Value.Str("again"))) {
        Ok(changed) => { fmt.Println("duplicate ok") }
        Err(e) => { fmt.Println(f"duplicate {kind_name(e.Kind)}") }
    }
    match conn.Exec("SELEC 1", none()) {
        Ok(changed) => { fmt.Println("bad ok") }
        Err(e) => { fmt.Println(f"bad {kind_name(e.Kind)}") }
    }
    match conn.Exec("SELECT ?", pair(Value.Num(1.0), Value.Num(2.0))) {
        Ok(changed) => { fmt.Println("param ok") }
        Err(e) => { fmt.Println(f"param {kind_name(e.Kind)}") }
    }
}

func cleanup(conn: db.Conn) -> Result<(), db.Error> {
    try conn.Exec(f"DROP DATABASE {DB}", none())
    fmt.Println("cleanup ok")
    return Result.Ok()
}

func run(conn: db.Conn) -> Result<(), db.Error> {
    try setup(conn)
    try read(conn)
    try prepared(conn)
    failures(conn)
    try cleanup(conn)
    return Result.Ok()
}

func main() {
    var dsn = DSN_DEFAULT
    match os.Env("ZYX_MYSQL_DSN") {
        Some(v) => { dsn = v }
        None => { }
    }
    match mysql.Open(dsn) {
        Ok(conn) => {
            match run(conn) {
                Ok(_) => { }
                Err(e) => { fmt.Println(f"failed {e.Msg}") }
            }
            conn.Close()
        }
        Err(e) => { fmt.Println(f"open failed {kind_name(e.Kind)}") }
    }
    match mysql.Open(dsn) {
        Ok(other) => {
            other.Close()
            match other.Exec("SELECT 1", none()) {
                Ok(changed) => { fmt.Println("closed ok") }
                Err(e) => { fmt.Println(f"closed {kind_name(e.Kind)}") }
            }
        }
        Err(e) => { fmt.Println("closed open failed") }
    }
}
`

// mysqlServerWant is what the program prints when every path of std/mysql
// works against the server.
const mysqlServerWant = `setup ok
inserts ok
columns 5
column id
column name
column amount
column note
column big
row id 1 name Ada amount 1.5 note none big 10 null true
row id 2 name O'Brien amount 0 note zero big 0 null false
row id 3 name gamma amount 2.25 note wide big 9223372036854775807 null false
by id O'Brien
by name 3
by null 1
zero 0
wide 9223372036854775807
text O'Brien
wide text 5000
duplicate Exists
bad Invalid
param Invalid
cleanup ok
closed Closed
`

// TestMySQLServer builds and runs a program against the MySQL server the
// environment names and compares its output byte for byte. Nothing answers,
// the suite skips rather than fails, so a machine without a server keeps the
// gate green.
func TestMySQLServer(t *testing.T) {
	tc, err := toolchain.Locate()
	if err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dsn := mysqlServerDSN()
	address := mysqlServerAddress(dsn)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Skipf("no MySQL server answers at %s: %v", address, err)
	}
	conn.Close()

	// The client library is a shared object, so the directory of the compiler
	// is put where the program can find it as well.
	if tc.Clang != "" {
		t.Setenv("PATH", filepath.Dir(tc.Clang)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	t.Setenv("ZYX_MYSQL_DSN", dsn)

	root := mysqlServerRoot(t)
	dir := t.TempDir()
	write := map[string]string{
		"zyx.mod":  "module example.com/mysqlserver\n\nzyx 0.1\n",
		"main.zyx": mysqlServerProgram,
	}
	for name, body := range write {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := mysqlServerRun(t, root, dir, "run", "-l", ":libmariadb.dll.a", "./...")
	if code != 0 {
		t.Fatalf("run failed with %d:\n%s", code, stderr)
	}
	if stdout != mysqlServerWant {
		t.Errorf("output =\n%s\nwant\n%s", stdout, mysqlServerWant)
	}
}
