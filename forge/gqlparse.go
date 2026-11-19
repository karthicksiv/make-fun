package main

// A GraphQL document parser: the October 2021 query language (operations, fragments, variables,
// directives, every value literal), enough for any document a client can send.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type gqlTokenKind int

const (
	tokEOF gqlTokenKind = iota
	tokPunct
	tokName
	tokInt
	tokFloat
	tokString
)

type gqlToken struct {
	kind gqlTokenKind
	text string // the punctuator, the name, the number as written, or the decoded string
	line int
	col  int
}

type gqlLexer struct {
	src  string
	pos  int
	line int
	col  int
}

type gqlSyntaxError struct {
	Message string
	Line    int
	Col     int
}

func (e *gqlSyntaxError) Error() string { return e.Message }

func (l *gqlLexer) errorf(format string, args ...any) *gqlSyntaxError {
	return &gqlSyntaxError{Message: fmt.Sprintf(format, args...), Line: l.line, Col: l.col}
}

func (l *gqlLexer) advance(n int) {
	for i := 0; i < n && l.pos < len(l.src); i++ {
		if l.src[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }

func (l *gqlLexer) skipIgnored() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			l.advance(1)
		case c == 0xEF && strings.HasPrefix(l.src[l.pos:], "\xEF\xBB\xBF"): // a byte order mark
			l.advance(3)
		case c == '#':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.advance(1)
			}
		default:
			return
		}
	}
}

func (l *gqlLexer) next() (gqlToken, *gqlSyntaxError) {
	l.skipIgnored()
	tok := gqlToken{line: l.line, col: l.col}
	if l.pos >= len(l.src) {
		tok.kind = tokEOF
		return tok, nil
	}
	c := l.src[l.pos]
	switch {
	case isNameStart(c):
		start := l.pos
		for l.pos < len(l.src) && isNameChar(l.src[l.pos]) {
			l.advance(1)
		}
		tok.kind, tok.text = tokName, l.src[start:l.pos]
		return tok, nil
	case c == '-' || (c >= '0' && c <= '9'):
		start := l.pos
		isFloat := false
		if c == '-' {
			l.advance(1)
		}
		if l.pos >= len(l.src) || l.src[l.pos] < '0' || l.src[l.pos] > '9' {
			return tok, l.errorf("Parse error on %q (error) at [%d, %d]", "-", tok.line, tok.col)
		}
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.advance(1)
		}
		if l.pos < len(l.src) && l.src[l.pos] == '.' {
			isFloat = true
			l.advance(1)
			for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
				l.advance(1)
			}
		}
		if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
			isFloat = true
			l.advance(1)
			if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
				l.advance(1)
			}
			for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
				l.advance(1)
			}
		}
		tok.text = l.src[start:l.pos]
		if isFloat {
			tok.kind = tokFloat
		} else {
			tok.kind = tokInt
		}
		return tok, nil
	case c == '"':
		text, err := l.readString()
		if err != nil {
			return tok, err
		}
		tok.kind, tok.text = tokString, text
		return tok, nil
	case c == '.':
		if strings.HasPrefix(l.src[l.pos:], "...") {
			l.advance(3)
			tok.kind, tok.text = tokPunct, "..."
			return tok, nil
		}
	case strings.IndexByte("!$&()[]{}:=@|", c) >= 0:
		l.advance(1)
		tok.kind, tok.text = tokPunct, string(c)
		return tok, nil
	}
	return tok, l.errorf("Parse error on %q (error) at [%d, %d]", string(c), tok.line, tok.col)
}

func (l *gqlLexer) readString() (string, *gqlSyntaxError) {
	if strings.HasPrefix(l.src[l.pos:], `"""`) {
		l.advance(3)
		start := l.pos
		for {
			if l.pos >= len(l.src) {
				return "", l.errorf("Unterminated block string")
			}
			if strings.HasPrefix(l.src[l.pos:], `\"""`) {
				l.advance(4)
				continue
			}
			if strings.HasPrefix(l.src[l.pos:], `"""`) {
				raw := l.src[start:l.pos]
				l.advance(3)
				return blockStringValue(strings.ReplaceAll(raw, `\"""`, `"""`)), nil
			}
			l.advance(1)
		}
	}
	l.advance(1)
	var b strings.Builder
	for {
		if l.pos >= len(l.src) || l.src[l.pos] == '\n' {
			return "", l.errorf("Unterminated string")
		}
		c := l.src[l.pos]
		if c == '"' {
			l.advance(1)
			return b.String(), nil
		}
		if c != '\\' {
			_, size := utf8.DecodeRuneInString(l.src[l.pos:])
			b.WriteString(l.src[l.pos : l.pos+size])
			l.advance(size)
			continue
		}
		l.advance(1)
		if l.pos >= len(l.src) {
			return "", l.errorf("Unterminated string")
		}
		esc := l.src[l.pos]
		l.advance(1)
		switch esc {
		case '"', '\\', '/':
			b.WriteByte(esc)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			if l.pos+4 > len(l.src) {
				return "", l.errorf("Bad unicode escape in string")
			}
			code, err := strconv.ParseUint(l.src[l.pos:l.pos+4], 16, 32)
			if err != nil {
				return "", l.errorf("Bad unicode escape in string")
			}
			l.advance(4)
			r := rune(code)
			// A surrogate pair written as two \u escapes.
			if r >= 0xD800 && r < 0xDC00 && strings.HasPrefix(l.src[l.pos:], `\u`) && l.pos+6 <= len(l.src) {
				if low, err := strconv.ParseUint(l.src[l.pos+2:l.pos+6], 16, 32); err == nil && low >= 0xDC00 && low < 0xE000 {
					r = 0x10000 + (r-0xD800)<<10 + (rune(low) - 0xDC00)
					l.advance(6)
				}
			}
			b.WriteRune(r)
		default:
			return "", l.errorf("Bad escape \\%c in string", esc)
		}
	}
}

// blockStringValue applies the spec's common-indentation and blank-line trimming.
func blockStringValue(raw string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), "\n")
	common := -1
	for i, line := range lines {
		if i == 0 {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < len(line) && (common < 0 || indent < common) {
			common = indent
		}
	}
	if common > 0 {
		for i := 1; i < len(lines); i++ {
			if len(lines[i]) >= common {
				lines[i] = lines[i][common:]
			} else {
				lines[i] = ""
			}
		}
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------- the syntax tree

type gqlValueKind int

const (
	valVariable gqlValueKind = iota
	valInt
	valFloat
	valString
	valBoolean
	valNull
	valEnum
	valList
	valObject
)

type gqlValue struct {
	Kind   gqlValueKind
	Text   string // variable name, number text, string, enum name, "true"/"false"
	List   []*gqlValue
	Fields []gqlObjectField
}

type gqlObjectField struct {
	Name  string
	Value *gqlValue
}

type gqlTypeRef struct {
	Name    string      // named type, when Of is nil
	Of      *gqlTypeRef // list element type, when this is a list
	NonNull bool
}

func (t *gqlTypeRef) String() string {
	var s string
	if t.Of != nil {
		s = "[" + t.Of.String() + "]"
	} else {
		s = t.Name
	}
	if t.NonNull {
		s += "!"
	}
	return s
}

type gqlArgument struct {
	Name  string
	Value *gqlValue
	Line  int
	Col   int
}

type gqlDirective struct {
	Name string
	Args []gqlArgument
}

type gqlSelection struct {
	// Exactly one of the three shapes: a field, a fragment spread, or an inline fragment.
	Field          *gqlFieldNode
	FragmentSpread string
	InlineFragment *gqlInlineFragment
	Directives     []gqlDirective
	Line           int
	Col            int
}

type gqlFieldNode struct {
	Alias      string
	Name       string
	Args       []gqlArgument
	Selections []*gqlSelection
}

func (f *gqlFieldNode) ResponseKey() string {
	if f.Alias != "" {
		return f.Alias
	}
	return f.Name
}

type gqlInlineFragment struct {
	TypeCondition string
	Selections    []*gqlSelection
}

type gqlVariableDef struct {
	Name    string
	Type    *gqlTypeRef
	Default *gqlValue
}

type gqlOperation struct {
	Type       string // query, mutation, subscription
	Name       string
	Variables  []gqlVariableDef
	Directives []gqlDirective
	Selections []*gqlSelection
	Line       int
	Col        int
}

type gqlFragmentDef struct {
	Name          string
	TypeCondition string
	Selections    []*gqlSelection
}

type gqlDocument struct {
	Operations []*gqlOperation
	Fragments  map[string]*gqlFragmentDef
}

// ---------------------------------------------------------------- the parser

type gqlParser struct {
	lex *gqlLexer
	tok gqlToken
}

func parseGraphQL(src string) (*gqlDocument, *gqlSyntaxError) {
	p := &gqlParser{lex: &gqlLexer{src: src, line: 1, col: 1}}
	if err := p.advance(); err != nil {
		return nil, err
	}
	doc := &gqlDocument{Fragments: map[string]*gqlFragmentDef{}}
	for p.tok.kind != tokEOF {
		if p.isName("fragment") {
			frag, err := p.parseFragment()
			if err != nil {
				return nil, err
			}
			doc.Fragments[frag.Name] = frag
			continue
		}
		op, err := p.parseOperation()
		if err != nil {
			return nil, err
		}
		doc.Operations = append(doc.Operations, op)
	}
	if len(doc.Operations) == 0 && len(doc.Fragments) == 0 {
		return nil, &gqlSyntaxError{Message: "Unexpected end of document", Line: 1, Col: 1}
	}
	return doc, nil
}

func (p *gqlParser) advance() *gqlSyntaxError {
	tok, err := p.lex.next()
	if err != nil {
		return err
	}
	p.tok = tok
	return nil
}

func (p *gqlParser) isPunct(text string) bool { return p.tok.kind == tokPunct && p.tok.text == text }
func (p *gqlParser) isName(text string) bool  { return p.tok.kind == tokName && p.tok.text == text }

func (p *gqlParser) unexpected() *gqlSyntaxError {
	what := p.tok.text
	kind := "error"
	switch p.tok.kind {
	case tokEOF:
		return &gqlSyntaxError{Message: "Unexpected end of document", Line: p.tok.line, Col: p.tok.col}
	case tokName:
		kind = "IDENTIFIER"
	case tokInt:
		kind = "INT"
	case tokFloat:
		kind = "FLOAT"
	case tokString:
		kind = "STRING"
	case tokPunct:
		kind = map[string]string{"{": "LCURLY", "}": "RCURLY", "(": "LPAREN", ")": "RPAREN", "[": "LBRACKET", "]": "RBRACKET", ":": "COLON", "!": "BANG", "$": "VAR_SIGN", "@": "DIR_SIGN", "=": "EQUALS", "...": "ELLIPSIS", "|": "PIPE", "&": "AMP"}[p.tok.text]
	}
	return &gqlSyntaxError{
		Message: fmt.Sprintf("Parse error on %q (%s) at [%d, %d]", what, kind, p.tok.line, p.tok.col),
		Line:    p.tok.line, Col: p.tok.col,
	}
}

func (p *gqlParser) expectPunct(text string) *gqlSyntaxError {
	if !p.isPunct(text) {
		return p.unexpected()
	}
	return p.advance()
}

func (p *gqlParser) expectName() (string, *gqlSyntaxError) {
	if p.tok.kind != tokName {
		return "", p.unexpected()
	}
	name := p.tok.text
	return name, p.advance()
}

func (p *gqlParser) parseOperation() (*gqlOperation, *gqlSyntaxError) {
	op := &gqlOperation{Type: "query", Line: p.tok.line, Col: p.tok.col}
	if p.isPunct("{") {
		sels, err := p.parseSelectionSet()
		if err != nil {
			return nil, err
		}
		op.Selections = sels
		return op, nil
	}
	if !(p.isName("query") || p.isName("mutation") || p.isName("subscription")) {
		return nil, p.unexpected()
	}
	op.Type = p.tok.text
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.tok.kind == tokName {
		op.Name = p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	if p.isPunct("(") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		for !p.isPunct(")") {
			if err := p.expectPunct("$"); err != nil {
				return nil, err
			}
			name, err := p.expectName()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct(":"); err != nil {
				return nil, err
			}
			typ, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			def := gqlVariableDef{Name: name, Type: typ}
			if p.isPunct("=") {
				if err := p.advance(); err != nil {
					return nil, err
				}
				value, err := p.parseValue(true)
				if err != nil {
					return nil, err
				}
				def.Default = value
			}
			if _, err := p.parseDirectives(); err != nil {
				return nil, err
			}
			op.Variables = append(op.Variables, def)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	dirs, err := p.parseDirectives()
	if err != nil {
		return nil, err
	}
	op.Directives = dirs
	sels, err := p.parseSelectionSet()
	if err != nil {
		return nil, err
	}
	op.Selections = sels
	return op, nil
}

func (p *gqlParser) parseFragment() (*gqlFragmentDef, *gqlSyntaxError) {
	if err := p.advance(); err != nil { // "fragment"
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if !p.isName("on") {
		return nil, p.unexpected()
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	cond, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if _, err := p.parseDirectives(); err != nil {
		return nil, err
	}
	sels, err := p.parseSelectionSet()
	if err != nil {
		return nil, err
	}
	return &gqlFragmentDef{Name: name, TypeCondition: cond, Selections: sels}, nil
}

func (p *gqlParser) parseTypeRef() (*gqlTypeRef, *gqlSyntaxError) {
	var t *gqlTypeRef
	if p.isPunct("[") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		inner, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct("]"); err != nil {
			return nil, err
		}
		t = &gqlTypeRef{Of: inner}
	} else {
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		t = &gqlTypeRef{Name: name}
	}
	if p.isPunct("!") {
		t.NonNull = true
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (p *gqlParser) parseDirectives() ([]gqlDirective, *gqlSyntaxError) {
	var out []gqlDirective
	for p.isPunct("@") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		args, err := p.parseArguments()
		if err != nil {
			return nil, err
		}
		out = append(out, gqlDirective{Name: name, Args: args})
	}
	return out, nil
}

func (p *gqlParser) parseArguments() ([]gqlArgument, *gqlSyntaxError) {
	if !p.isPunct("(") {
		return nil, nil
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	var out []gqlArgument
	for !p.isPunct(")") {
		arg := gqlArgument{Line: p.tok.line, Col: p.tok.col}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		arg.Name = name
		if err := p.expectPunct(":"); err != nil {
			return nil, err
		}
		value, err := p.parseValue(false)
		if err != nil {
			return nil, err
		}
		arg.Value = value
		out = append(out, arg)
	}
	return out, p.advance()
}

func (p *gqlParser) parseSelectionSet() ([]*gqlSelection, *gqlSyntaxError) {
	if err := p.expectPunct("{"); err != nil {
		return nil, err
	}
	var out []*gqlSelection
	for !p.isPunct("}") {
		sel := &gqlSelection{Line: p.tok.line, Col: p.tok.col}
		if p.isPunct("...") {
			if err := p.advance(); err != nil {
				return nil, err
			}
			if p.tok.kind == tokName && p.tok.text != "on" {
				sel.FragmentSpread = p.tok.text
				if err := p.advance(); err != nil {
					return nil, err
				}
				dirs, err := p.parseDirectives()
				if err != nil {
					return nil, err
				}
				sel.Directives = dirs
			} else {
				inline := &gqlInlineFragment{}
				if p.isName("on") {
					if err := p.advance(); err != nil {
						return nil, err
					}
					cond, err := p.expectName()
					if err != nil {
						return nil, err
					}
					inline.TypeCondition = cond
				}
				dirs, err := p.parseDirectives()
				if err != nil {
					return nil, err
				}
				sel.Directives = dirs
				sels, err := p.parseSelectionSet()
				if err != nil {
					return nil, err
				}
				inline.Selections = sels
				sel.InlineFragment = inline
			}
			out = append(out, sel)
			continue
		}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		field := &gqlFieldNode{Name: name}
		if p.isPunct(":") {
			if err := p.advance(); err != nil {
				return nil, err
			}
			real, err := p.expectName()
			if err != nil {
				return nil, err
			}
			field.Alias, field.Name = name, real
		}
		args, err := p.parseArguments()
		if err != nil {
			return nil, err
		}
		field.Args = args
		dirs, err := p.parseDirectives()
		if err != nil {
			return nil, err
		}
		sel.Directives = dirs
		if p.isPunct("{") {
			sels, err := p.parseSelectionSet()
			if err != nil {
				return nil, err
			}
			field.Selections = sels
		}
		sel.Field = field
		out = append(out, sel)
	}
	if len(out) == 0 {
		return nil, p.unexpected()
	}
	return out, p.advance()
}

func (p *gqlParser) parseValue(constant bool) (*gqlValue, *gqlSyntaxError) {
	switch p.tok.kind {
	case tokInt:
		v := &gqlValue{Kind: valInt, Text: p.tok.text}
		return v, p.advance()
	case tokFloat:
		v := &gqlValue{Kind: valFloat, Text: p.tok.text}
		return v, p.advance()
	case tokString:
		v := &gqlValue{Kind: valString, Text: p.tok.text}
		return v, p.advance()
	case tokName:
		v := &gqlValue{Kind: valEnum, Text: p.tok.text}
		switch p.tok.text {
		case "true", "false":
			v.Kind = valBoolean
		case "null":
			v.Kind = valNull
		}
		return v, p.advance()
	case tokPunct:
		switch p.tok.text {
		case "$":
			if constant {
				return nil, p.unexpected()
			}
			if err := p.advance(); err != nil {
				return nil, err
			}
			name, err := p.expectName()
			if err != nil {
				return nil, err
			}
			return &gqlValue{Kind: valVariable, Text: name}, nil
		case "[":
			if err := p.advance(); err != nil {
				return nil, err
			}
			v := &gqlValue{Kind: valList}
			for !p.isPunct("]") {
				item, err := p.parseValue(constant)
				if err != nil {
					return nil, err
				}
				v.List = append(v.List, item)
			}
			return v, p.advance()
		case "{":
			if err := p.advance(); err != nil {
				return nil, err
			}
			v := &gqlValue{Kind: valObject}
			for !p.isPunct("}") {
				name, err := p.expectName()
				if err != nil {
					return nil, err
				}
				if err := p.expectPunct(":"); err != nil {
					return nil, err
				}
				item, err := p.parseValue(constant)
				if err != nil {
					return nil, err
				}
				v.Fields = append(v.Fields, gqlObjectField{Name: name, Value: item})
			}
			return v, p.advance()
		}
	}
	return nil, p.unexpected()
}
