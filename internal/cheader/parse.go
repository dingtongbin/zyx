// The syntax tree walker and the C type mapping.

package cheader

import (
	"strconv"
	"strings"

	"zyx/internal/types"
)

// A node is the part of clang's syntax tree this package reads. Everything
// else the tree carries is left behind by the decoder, which is what keeps a
// large header from costing more than the declarations in it.
type node struct {
	Kind               string  `json:"kind"`
	Name               string  `json:"name"`
	TagUsed            string  `json:"tagUsed"`
	Variadic           bool    `json:"variadic"`
	CompleteDefinition bool    `json:"completeDefinition"`
	Value              string  `json:"value"`
	Type               ctype   `json:"type"`
	Inner              []*node `json:"inner"`
}

// A ctype is the type a declaration carries, in the spelling clang writes
// and, when a typedef was resolved, in the spelling underneath it.
type ctype struct {
	QualType          string `json:"qualType"`
	DesugaredQualType string `json:"desugaredQualType"`
}

// spelling returns the type to read: the resolved form when clang wrote one,
// so a parameter declared with a typedef maps the way its target does.
func (c ctype) spelling() string {
	if c.DesugaredQualType != "" {
		return c.DesugaredQualType
	}
	return c.QualType
}

// parser walks one syntax tree and fills a table.
type parser struct {
	sizes widths
	table *Table
}

// walk reads the declarations of a translation unit. Only the top level and
// the members of a record are read: a local of an inline function is not a
// declaration the c namespace exposes.
func (p *parser) walk(n *node) {
	switch n.Kind {
	case "FunctionDecl":
		p.function(n)
		return
	case "RecordDecl":
		p.record(n)
		for _, child := range n.Inner {
			switch child.Kind {
			case "RecordDecl", "EnumDecl", "TypedefDecl":
				p.walk(child)
			}
		}
		return
	case "TypedefDecl":
		p.alias(n)
		return
	case "EnumDecl":
		p.enum(n)
		return
	case "VarDecl":
		p.global(n)
		return
	}
	for _, child := range n.Inner {
		p.walk(child)
	}
}

// function records one function declaration.
func (p *parser) function(n *node) {
	if n.Name == "" {
		return
	}
	fn := &Func{Name: n.Name, Variadic: n.Variadic}
	for _, child := range n.Inner {
		if child.Kind != "ParmVarDecl" {
			continue
		}
		fn.Params = append(fn.Params, p.cType(child.Type.spelling()))
		fn.Names = append(fn.Names, child.Name)
	}
	if result := resultOf(n.Type.spelling()); result != "void" && result != "" {
		fn.Result = p.cType(result)
	}
	p.table.Functions[n.Name] = fn
}

// record records one struct or union tag.
func (p *parser) record(n *node) {
	if n.Name == "" || !n.CompleteDefinition {
		return
	}
	p.table.Records[n.Name] = &Record{Name: n.Name, Union: n.TagUsed == "union"}
}

// alias records one typedef.
func (p *parser) alias(n *node) {
	if n.Name == "" {
		return
	}
	if t := p.cType(n.Type.spelling()); t != nil {
		p.table.Aliases[n.Name] = t
	}
}

// enum records the enumerators of one enumeration. A C enumerator is an
// integer constant, which is what a program reads it as.
func (p *parser) enum(n *node) {
	for _, child := range n.Inner {
		if child.Kind != "EnumConstantDecl" || child.Name == "" {
			continue
		}
		for _, inner := range child.Inner {
			if inner.Kind != "ConstantExpr" || inner.Value == "" {
				continue
			}
			if v, err := strconv.ParseInt(inner.Value, 0, 64); err == nil {
				p.table.Constants[child.Name] = v
			}
			break
		}
	}
}

// global records one global object.
func (p *parser) global(n *node) {
	if n.Name == "" {
		return
	}
	if t := p.cType(n.Type.spelling()); t != nil {
		p.table.Globals[n.Name] = t
	}
}

// resultOf returns the result of a function type spelling, which is what
// stands before the parameter list. The parameter list is the last group of
// parentheses that is not nested in another, so a parameter that is itself a
// function pointer does not end the search.
func resultOf(spelling string) string {
	depth, cut := 0, -1
	for i := 0; i < len(spelling); i++ {
		switch spelling[i] {
		case '(':
			if depth == 0 {
				cut = i
			}
			depth++
		case ')':
			depth--
		}
	}
	if cut < 0 {
		return strings.TrimSpace(spelling)
	}
	return strings.TrimSpace(spelling[:cut])
}

// cType maps a C type spelling onto the language type it crosses as.
//
// An address crosses as raw, including the address of a function: a C
// function pointer has no receiver and one word, which is exactly what raw
// is, so a callback field needs no type of its own. A record crosses as the
// extern type of its tag, so a declaration a program writes and the type the
// header names are the same type.
func (p *parser) cType(spelling string) *types.Type {
	text := normalize(spelling)
	switch text {
	case "void":
		return nil
	case "bool", "_Bool":
		return types.Bool
	case "char", "signed char":
		return p.intType(p.sizes.char, true)
	case "unsigned char":
		return p.intType(p.sizes.char, false)
	case "short", "short int", "signed short", "signed short int":
		return p.intType(p.sizes.short, true)
	case "unsigned short", "unsigned short int":
		return p.intType(p.sizes.short, false)
	case "int", "signed", "signed int":
		return p.intType(p.sizes.int, true)
	case "unsigned", "unsigned int":
		return p.intType(p.sizes.int, false)
	case "long", "long int", "signed long", "signed long int":
		return p.intType(p.sizes.long, true)
	case "unsigned long", "unsigned long int":
		return p.intType(p.sizes.long, false)
	case "long long", "long long int", "signed long long", "signed long long int":
		return p.intType(p.sizes.longLong, true)
	case "unsigned long long", "unsigned long long int":
		return p.intType(p.sizes.longLong, false)
	case "float":
		return types.Float32
	case "double", "long double":
		return types.Float64
	}
	if strings.Contains(text, "(*)") || strings.HasSuffix(text, "*") {
		// An address crosses as raw, including the address of a function: a
		// C function pointer has no receiver and one word, which is what raw
		// is.
		return types.Raw
	}
	if name, ok := tagOf(text, "struct"); ok {
		return types.Named(types.KindExternStruct, "", "", name, 0)
	}
	if name, ok := tagOf(text, "union"); ok {
		return types.Named(types.KindUnion, "", "", name, 0)
	}
	if name, ok := tagOf(text, "enum"); ok {
		// A C enumerator is an integer, so the enumeration crosses as one.
		_ = name
		return types.Int32
	}
	if alias, ok := p.table.Aliases[text]; ok {
		return alias
	}
	if record, ok := p.table.Records[text]; ok {
		kind := types.KindExternStruct
		if record.Union {
			kind = types.KindUnion
		}
		return types.Named(kind, "", "", text, 0)
	}
	return nil
}

// tagOf returns the tag of a `struct X`, `union X`, or `enum X` spelling.
func tagOf(text, keyword string) (string, bool) {
	rest, ok := strings.CutPrefix(text, keyword+" ")
	if !ok {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	return rest, rest != ""
}

// intType returns the language integer of a C width.
func (p *parser) intType(size int, signed bool) *types.Type {
	if signed {
		switch size {
		case 1:
			return types.Int8
		case 2:
			return types.Int16
		case 4:
			return types.Int32
		case 8:
			return types.Int64
		case 16:
			return types.Int128
		}
		return nil
	}
	switch size {
	case 1:
		return types.Uint8
	case 2:
		return types.Uint16
	case 4:
		return types.Uint32
	case 8:
		return types.Uint64
	case 16:
		return types.Uint128
	}
	return nil
}

// qualifiers lists the C spellings that do not change the type a value has.
var qualifiers = []string{"const ", "volatile ", "restrict ", "__restrict ", "_Atomic "}

// normalize returns a C type spelling without the qualifiers that leave the
// type alone and with one space between the words that remain.
func normalize(spelling string) string {
	text := " " + strings.Join(strings.Fields(spelling), " ") + " "
	for _, q := range qualifiers {
		text = strings.ReplaceAll(text, " "+q, " ")
	}
	return strings.TrimSpace(text)
}
