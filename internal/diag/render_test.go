package diag

import (
	"strings"
	"testing"

	"zyx/internal/source"
)

func TestRenderString(t *testing.T) {
	file := source.NewFile("main.zyx", []byte("package main\nvar x = @\n"))
	span := source.Span{File: file, Start: 21, End: 22}
	d := New(CodeUnexpectedChar, span, "unexpected character '@'")
	want := "main.zyx:2:9: E1001 unexpected character '@'\n"
	if got := d.RenderString(); got != want {
		t.Fatalf("RenderString() = %q, want %q", got, want)
	}
}

func TestRenderStringWithHelp(t *testing.T) {
	file := source.NewFile("main.zyx", []byte("a::b\n"))
	span := source.Span{File: file, Start: 1, End: 3}
	d := New(CodeDoubleColon, span, "'::' is not valid in zyx").
		WithHelp("use '.' for member access")
	want := "main.zyx:1:2: E1002 '::' is not valid in zyx\n" +
		"    help: use '.' for member access\n"
	if got := d.RenderString(); got != want {
		t.Fatalf("RenderString() = %q, want %q", got, want)
	}
}

func TestRenderStringWithNote(t *testing.T) {
	file := source.NewFile("main.zyx", []byte("var a = 1\nvar a = 2\n"))
	span := source.Span{File: file, Start: 10, End: 15}
	note := source.Span{File: file, Start: 0, End: 3}
	d := New("E9999", span, "duplicate declaration").
		WithNote(note, "first declared here")
	want := "main.zyx:2:1: E9999 duplicate declaration\n" +
		"main.zyx:1:1: note: first declared here\n"
	if got := d.RenderString(); got != want {
		t.Fatalf("RenderString() = %q, want %q", got, want)
	}
	if !strings.Contains(d.RenderString(), "note:") {
		t.Fatal("note line missing from rendered diagnostic")
	}
}
