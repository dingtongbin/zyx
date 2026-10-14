package source

import "testing"

func TestNewFileLineMap(t *testing.T) {
	f := NewFile("main.zyx", []byte("abc\nde\n"))
	if got, want := f.LineCount(), 3; got != want {
		t.Fatalf("LineCount = %d, want %d", got, want)
	}
	cases := []struct {
		offset    int
		line, col int
	}{
		{0, 1, 1},
		{3, 1, 4},
		{4, 2, 1},
		{6, 2, 3},
		{7, 3, 1},
	}
	for _, c := range cases {
		line, col := f.LineCol(c.offset)
		if line != c.line || col != c.col {
			t.Errorf("LineCol(%d) = (%d, %d), want (%d, %d)",
				c.offset, line, col, c.line, c.col)
		}
	}
}

func TestLineColCountsRunes(t *testing.T) {
	// UTF-8 handling test data written as a Go escape to keep this file ASCII.
	f := NewFile("main.zyx", []byte("x=\"\u4E2D\"\n"))
	cases := []struct {
		offset    int
		line, col int
	}{
		{3, 1, 4}, // the opening quote of the literal
		{6, 1, 5}, // the closing quote, one column past the rune
		{7, 1, 6},
	}
	for _, c := range cases {
		line, col := f.LineCol(c.offset)
		if line != c.line || col != c.col {
			t.Errorf("LineCol(%d) = (%d, %d), want (%d, %d)",
				c.offset, line, col, c.line, c.col)
		}
	}
}

func TestNewFileStripsBOM(t *testing.T) {
	f := NewFile("main.zyx", []byte("\xEF\xBB\xBFhi\n"))
	if got, want := string(f.Data()), "hi\n"; got != want {
		t.Fatalf("Data = %q, want %q", got, want)
	}
	if got, want := f.LineCount(), 2; got != want {
		t.Fatalf("LineCount = %d, want %d", got, want)
	}
	line, col := f.LineCol(0)
	if line != 1 || col != 1 {
		t.Fatalf("LineCol(0) = (%d, %d), want (1, 1)", line, col)
	}
}

func TestNewFileEmpty(t *testing.T) {
	f := NewFile("main.zyx", nil)
	if got, want := f.LineCount(), 1; got != want {
		t.Fatalf("LineCount = %d, want %d", got, want)
	}
	line, col := f.LineCol(0)
	if line != 1 || col != 1 {
		t.Fatalf("LineCol(0) = (%d, %d), want (1, 1)", line, col)
	}
	if f.Name() != "main.zyx" {
		t.Fatalf("Name = %q, want %q", f.Name(), "main.zyx")
	}
}

func TestLineColOutOfRangePanics(t *testing.T) {
	f := NewFile("main.zyx", []byte("ab"))
	defer func() {
		if recover() == nil {
			t.Fatal("LineCol(3) did not panic on a two-byte file")
		}
	}()
	f.LineCol(3)
}

func TestSpanTextAndPos(t *testing.T) {
	// UTF-8 handling test data written as a Go escape to keep this file ASCII.
	f := NewFile("main.zyx", []byte("a = \"\u4E2D\"\n"))
	s := Span{File: f, Start: 5, End: 8}
	if got, want := s.Text(), "\u4E2D"; got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
	if got, want := s.Len(), 3; got != want {
		t.Fatalf("Len = %d, want %d", got, want)
	}
	line, col := s.StartPos()
	if line != 1 || col != 6 {
		t.Fatalf("StartPos = (%d, %d), want (1, 6)", line, col)
	}
}
