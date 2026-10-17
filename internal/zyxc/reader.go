// Reader for .zyxc.

package zyxc

import (
	"strconv"
	"strings"
)

// Parse reads the canonical text of one file. It accepts exactly what Format
// writes: the header lines, the summary, then the tree, one node per line
// indented two spaces per level and opening with its number. A header line the
// reader does not know is skipped, so the format can carry one more without a
// reader change.
func Parse(data []byte) (*File, error) {
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 4 {
		return nil, errf("a file needs the header lines")
	}
	f := &File{}
	version, ok := strings.CutPrefix(lines[0], "zyxc ")
	if !ok {
		return nil, errf("the first line must be zyxc <version>")
	}
	n, err := strconv.Atoi(version)
	if err != nil || n != FormatVersion {
		return nil, errf("format version %q is not the one this reader accepts", version)
	}
	if f.Target, ok = strings.CutPrefix(lines[1], "target "); !ok {
		return nil, errf("the second line must be target <triple>")
	}
	if f.Module, ok = strings.CutPrefix(lines[2], "module "); !ok {
		return nil, errf("the third line must be module <path>")
	}
	if f.Exports, ok = strings.CutPrefix(lines[3], "exports "); !ok {
		return nil, errf("the fourth line must be exports <hash>")
	}

	// The rest of the header carries counts the reader does not need, and the
	// tree is one of the sections below it.
	start := 4
	for start < len(lines) && !isSectionHeader(lines[start]) {
		start++
	}
	var stack []*Node
	inTree := false
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			return nil, errf("line %d is blank", i+1)
		}
		depth, err := depthOf(line)
		if err != nil {
			return nil, errf("line %d: %v", i+1, err)
		}
		if depth == 0 {
			switch strings.TrimSpace(line) {
			case "tree":
				inTree = true
			case "summary":
				inTree = false
			default:
				return nil, errf("line %d: %q is not a section", i+1, strings.TrimSpace(line))
			}
			stack = stack[:0]
			continue
		}
		if !inTree {
			continue
		}
		if depth > len(stack)+1 {
			return nil, errf("line %d indents past its parent", i+1)
		}
		text, err := cutNodeID(strings.TrimLeft(line, " "))
		if err != nil {
			return nil, errf("line %d: %v", i+1, err)
		}
		node, err := parseLine(text)
		if err != nil {
			return nil, errf("line %d: %v", i+1, err)
		}
		stack = stack[:depth-1]
		if depth == 1 {
			f.Decls = append(f.Decls, node)
		} else {
			parent := stack[depth-2]
			parent.Children = append(parent.Children, node)
		}
		stack = append(stack, node)
	}
	if !inTree {
		return nil, errf("the file has no tree section")
	}
	return f, nil
}

// isSectionHeader reports whether a line opens a section, which is where the
// header ends.
func isSectionHeader(line string) bool {
	switch strings.TrimSpace(line) {
	case "summary", "tree":
		return true
	}
	return false
}

// cutNodeID strips the number that opens a node line. It names the node rather
// than describing it, so a reader that does not need it drops it here.
func cutNodeID(text string) (string, error) {
	if !strings.HasPrefix(text, "n") {
		return "", errf("a node line opens with its number")
	}
	rest := text[1:]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits == len(rest) || rest[digits] != ' ' {
		return "", errf("a node number is n followed by digits and a space")
	}
	return rest[digits+1:], nil
}

// depthOf returns the indentation level of a line, which must be a whole
// number of two-space steps.
func depthOf(line string) (int, error) {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	if spaces%2 != 0 {
		return 0, errf("indentation is not a multiple of two spaces")
	}
	return spaces / 2, nil
}

// parseLine reads one node line: a kind, its payload tokens, an optional
// position, and optional marks.
func parseLine(line string) (*Node, error) {
	tail, marks, err := cutMarks(line)
	if err != nil {
		return nil, err
	}
	head, pos, err := cutPos(tail)
	if err != nil {
		return nil, err
	}
	head, from := cutFrom(head)
	tokens, err := splitTokens(head)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errf("a line needs a kind")
	}
	n := &Node{Kind: tokens[0], Payload: tokens[1:], Pos: pos, From: from, Marks: marks}
	if err := checkKind(n.Kind); err != nil {
		return nil, err
	}
	return n, nil
}

// cutMarks splits the trailing `marks {...}` section off a line. A mark
// argument is one token, quoted when it holds a space, so an argument that
// carries a type text keeps its comma.
func cutMarks(line string) (string, []Mark, error) {
	idx := markerOutside(line, " marks {", true)
	if idx < 0 {
		return line, nil, nil
	}
	section := line[idx+len(" marks {"):]
	if !strings.HasSuffix(section, "}") {
		return "", nil, errf("the marks section is not closed")
	}
	section = strings.TrimSuffix(section, "}")
	head := line[:idx]
	parts, err := splitMarkParts(section)
	if err != nil {
		return "", nil, err
	}
	var marks []Mark
	for _, part := range parts {
		if part == "" {
			return "", nil, errf("the marks section holds an empty entry")
		}
		name, argText := cutMarkHead(part)
		arg, err := oneToken(argText)
		if err != nil {
			return "", nil, err
		}
		marks = append(marks, Mark{Name: name, Arg: arg})
	}
	return head, marks, nil
}

// markerOutside returns where a marker stands in a line, looking only outside
// the quoted tokens: a payload token may hold any text, including the marker
// itself, and the section it opens would otherwise be read from there.
func markerOutside(line, marker string, last bool) int {
	found := -1
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && inQuote && i+1 < len(line):
			i++
		case c == '"':
			inQuote = !inQuote
		case !inQuote && strings.HasPrefix(line[i:], marker):
			found = i
			if !last {
				return i
			}
		}
	}
	return found
}

// splitMarkParts splits a marks section on the commas between marks. A comma
// inside a quoted argument belongs to the argument.
func splitMarkParts(section string) ([]string, error) {
	var out []string
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(section); i++ {
		c := section[i]
		switch {
		case c == '\\' && inQuote && i+1 < len(section):
			b.WriteByte(c)
			i++
			b.WriteByte(section[i])
		case c == '"':
			inQuote = !inQuote
			b.WriteByte(c)
		case c == ',' && !inQuote:
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if inQuote {
		return nil, errf("a quoted mark argument is not closed")
	}
	return append(out, strings.TrimSpace(b.String())), nil
}

// cutMarkHead splits one mark into its name and the text of its argument. The
// name runs to the first space outside quotes, and a mark without an argument
// has none.
func cutMarkHead(part string) (name, arg string) {
	inQuote := false
	for i := 0; i < len(part); i++ {
		switch c := part[i]; {
		case c == '\\' && inQuote && i+1 < len(part):
			i++
		case c == '"':
			inQuote = !inQuote
		case c == ' ' && !inQuote:
			return part[:i], strings.TrimSpace(part[i+1:])
		}
	}
	return part, ""
}

// oneToken reads a mark argument, which is one token.
func oneToken(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	tokens, err := splitTokens(text)
	if err != nil {
		return "", err
	}
	if len(tokens) != 1 {
		return "", errf("a mark argument is one token")
	}
	return tokens[0], nil
}

// cutPos splits the trailing `pos <position>` section off a line.
func cutPos(line string) (string, string, error) {
	idx := markerOutside(line, " pos ", true)
	if idx < 0 {
		return line, "", nil
	}
	return line[:idx], line[idx+len(" pos "):], nil
}

// cutFrom splits the trailing `from <id>` section off a line.
func cutFrom(line string) (string, string) {
	idx := markerOutside(line, " from ", true)
	if idx < 0 {
		return line, ""
	}
	return line[:idx], line[idx+len(" from "):]
}

// splitTokens splits a line into payload tokens, honoring double quotes.
func splitTokens(line string) ([]string, error) {
	var out []string
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && inQuote && i+1 < len(line):
			i++
			switch line[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '0':
				b.WriteByte(0)
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case 'u':
				size, err := readUnicodeEscape(line[i+1:], &b)
				if err != nil {
					return nil, err
				}
				i += size
			default:
				return nil, errf("unknown escape \\%c", line[i])
			}
		case c == '"':
			if inQuote {
				out = append(out, b.String())
				b.Reset()
				inQuote = false
				continue
			}
			inQuote = true
		case c == ' ' && !inQuote:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(c)
		}
	}
	if inQuote {
		return nil, errf("a quoted token is not closed")
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out, nil
}

// readUnicodeEscape reads the `{<hex>}` part of a \u escape and writes the
// code point it names. It answers how many bytes of the rest it consumed.
func readUnicodeEscape(rest string, b *strings.Builder) (int, error) {
	if rest == "" || rest[0] != '{' {
		return 0, errf("a \\u escape is written \\u{<hex>}")
	}
	end := strings.IndexByte(rest, '}')
	if end < 0 {
		return 0, errf("a \\u escape is not closed")
	}
	digits := rest[1:end]
	value, err := strconv.ParseUint(digits, 16, 32)
	if err != nil {
		return 0, errf("\\u{%s} is not a code point", digits)
	}
	b.WriteRune(rune(value))
	// The digits, the braces, and the escape letter are consumed; the caller
	// advances past what is left.
	return end + 1, nil
}
