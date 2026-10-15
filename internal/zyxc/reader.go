// Reader for .zyxc.

package zyxc

import (
	"strconv"
	"strings"
)

// Parse reads the canonical text of one file. It accepts exactly what
// Format writes: the four-line header, then one node per line indented two
// spaces per level.
func Parse(data []byte) (*File, error) {
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 4 {
		return nil, errf("a file needs the four header lines")
	}
	f := &File{}
	version, ok := strings.CutPrefix(lines[0], "zyx ")
	if !ok {
		return nil, errf("the first line must be zyx <version>")
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

	var stack []*Node
	for i := 4; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			return nil, errf("line %d is blank", i+1)
		}
		depth, err := depthOf(line)
		if err != nil {
			return nil, errf("line %d: %v", i+1, err)
		}
		if depth > len(stack) {
			return nil, errf("line %d indents past its parent", i+1)
		}
		node, err := parseLine(strings.TrimLeft(line, " "))
		if err != nil {
			return nil, errf("line %d: %v", i+1, err)
		}
		stack = stack[:depth]
		if depth == 0 {
			f.Decls = append(f.Decls, node)
		} else {
			parent := stack[depth-1]
			parent.Children = append(parent.Children, node)
		}
		stack = append(stack, node)
	}
	return f, nil
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
	tokens, err := splitTokens(head)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errf("a line needs a kind")
	}
	n := &Node{Kind: tokens[0], Payload: tokens[1:], Pos: pos, Marks: marks}
	if err := checkKind(n.Kind); err != nil {
		return nil, err
	}
	return n, nil
}

// cutMarks splits the trailing `marks {...}` section off a line. A mark
// argument is one token, quoted when it holds a space, so an argument that
// carries a type text keeps its comma.
func cutMarks(line string) (string, []Mark, error) {
	idx := strings.Index(line, " marks {")
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
	idx := strings.LastIndex(line, " pos ")
	if idx < 0 {
		return line, "", nil
	}
	return line[:idx], line[idx+len(" pos "):], nil
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
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
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
