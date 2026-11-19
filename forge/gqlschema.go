package main

// The GraphQL type system: a schema is written in SDL (schema definition language), parsed once at
// startup, and resolvers are attached to its fields by "Type.field" name. Introspection is answered
// from the same parsed schema, so what __type describes is exactly what executes.

import (
	"fmt"
	"sort"
	"strconv"
)

const (
	kindScalar      = "SCALAR"
	kindObject      = "OBJECT"
	kindInterface   = "INTERFACE"
	kindUnion       = "UNION"
	kindEnum        = "ENUM"
	kindInputObject = "INPUT_OBJECT"
	kindList        = "LIST"
	kindNonNull     = "NON_NULL"
)

// gqlResolveFn computes one field. parent is whatever the enclosing field resolved to.
type gqlResolveFn func(c *gqlExec, parent any, args map[string]any) (any, error)

type gqlSchemaArg struct {
	Name        string
	Description string
	Type        *gqlTypeRef
	Default     *gqlValue
}

type gqlSchemaField struct {
	Name        string
	Description string
	Type        *gqlTypeRef
	Args        []*gqlSchemaArg
	Resolve     gqlResolveFn
	Deprecation string
	deprecated  bool
}

func (f *gqlSchemaField) arg(name string) *gqlSchemaArg {
	for _, a := range f.Args {
		if a.Name == name {
			return a
		}
	}
	return nil
}

type gqlEnumValue struct {
	Name        string
	Description string
}

type gqlSchemaType struct {
	Kind        string
	Name        string
	Description string
	Fields      []*gqlSchemaField
	fields      map[string]*gqlSchemaField
	Interfaces  []string
	Possible    []string // for an interface or a union: the object types it can be
	EnumValues  []gqlEnumValue
	InputFields []*gqlSchemaArg
}

func (t *gqlSchemaType) field(name string) *gqlSchemaField {
	if t == nil {
		return nil
	}
	return t.fields[name]
}

func (t *gqlSchemaType) inputField(name string) *gqlSchemaArg {
	for _, f := range t.InputFields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func (t *gqlSchemaType) isComposite() bool {
	return t != nil && (t.Kind == kindObject || t.Kind == kindInterface || t.Kind == kindUnion)
}

type gqlSchema struct {
	Types        map[string]*gqlSchemaType
	order        []string
	Query        string
	Mutation     string
	Subscription string
}

func (s *gqlSchema) typ(name string) *gqlSchemaType { return s.Types[name] }

// possibleType reports whether an object type satisfies a type condition (itself, an interface it
// implements, or a union it belongs to).
func (s *gqlSchema) possibleType(condition, object string) bool {
	if condition == object {
		return true
	}
	t := s.Types[condition]
	if t == nil {
		return false
	}
	for _, p := range t.Possible {
		if p == object {
			return true
		}
	}
	return false
}

// attach sets the resolver of "Type.field".
func (s *gqlSchema) attach(typeName, fieldName string, fn gqlResolveFn) {
	t := s.Types[typeName]
	if t == nil {
		panic("graphql: resolver for unknown type " + typeName)
	}
	f := t.fields[fieldName]
	if f == nil {
		panic("graphql: resolver for unknown field " + typeName + "." + fieldName)
	}
	f.Resolve = fn
}

// ---------------------------------------------------------------- SDL

// parseSchema reads SDL. It panics on malformed SDL: the schema is a constant of the program.
func parseSchema(sdl string) *gqlSchema {
	p := &gqlParser{lex: &gqlLexer{src: sdl, line: 1, col: 1}}
	must := func(err *gqlSyntaxError) {
		if err != nil {
			panic(fmt.Sprintf("graphql schema: %s (line %d)", err.Message, err.Line))
		}
	}
	must(p.advance())
	s := &gqlSchema{Types: map[string]*gqlSchemaType{}, Query: "Query", Mutation: "Mutation"}
	add := func(t *gqlSchemaType) {
		if _, dup := s.Types[t.Name]; dup {
			panic("graphql schema: type " + t.Name + " is defined twice")
		}
		t.fields = map[string]*gqlSchemaField{}
		for _, f := range t.Fields {
			t.fields[f.Name] = f
		}
		s.Types[t.Name] = t
		s.order = append(s.order, t.Name)
	}
	description := func() string {
		if p.tok.kind == tokString {
			text := p.tok.text
			must(p.advance())
			return text
		}
		return ""
	}
	name := func() string {
		n, err := p.expectName()
		must(err)
		return n
	}
	typeRef := func() *gqlTypeRef {
		t, err := p.parseTypeRef()
		must(err)
		return t
	}
	directives := func() (deprecated bool, reason string) {
		dirs, err := p.parseDirectives()
		must(err)
		for _, d := range dirs {
			if d.Name == "deprecated" {
				deprecated = true
				reason = "No longer supported"
				for _, a := range d.Args {
					if a.Name == "reason" && a.Value.Kind == valString {
						reason = a.Value.Text
					}
				}
			}
		}
		return
	}
	inputValues := func(open, close string) []*gqlSchemaArg {
		var out []*gqlSchemaArg
		if !p.isPunct(open) {
			return out
		}
		must(p.advance())
		for !p.isPunct(close) {
			a := &gqlSchemaArg{Description: description()}
			a.Name = name()
			must(p.expectPunct(":"))
			a.Type = typeRef()
			if p.isPunct("=") {
				must(p.advance())
				v, err := p.parseValue(true)
				must(err)
				a.Default = v
			}
			directives()
			out = append(out, a)
		}
		must(p.advance())
		return out
	}
	fields := func() []*gqlSchemaField {
		var out []*gqlSchemaField
		must(p.expectPunct("{"))
		for !p.isPunct("}") {
			f := &gqlSchemaField{Description: description()}
			f.Name = name()
			f.Args = inputValues("(", ")")
			must(p.expectPunct(":"))
			f.Type = typeRef()
			f.deprecated, f.Deprecation = directives()
			out = append(out, f)
		}
		must(p.advance())
		return out
	}
	for p.tok.kind != tokEOF {
		desc := description()
		keyword := name()
		switch keyword {
		case "schema":
			must(p.expectPunct("{"))
			for !p.isPunct("}") {
				op := name()
				must(p.expectPunct(":"))
				target := name()
				switch op {
				case "query":
					s.Query = target
				case "mutation":
					s.Mutation = target
				case "subscription":
					s.Subscription = target
				}
			}
			must(p.advance())
		case "scalar":
			t := &gqlSchemaType{Kind: kindScalar, Name: name(), Description: desc}
			directives()
			add(t)
		case "type", "interface":
			t := &gqlSchemaType{Kind: kindObject, Name: name(), Description: desc}
			if keyword == "interface" {
				t.Kind = kindInterface
			}
			if p.isName("implements") {
				must(p.advance())
				if p.isPunct("&") {
					must(p.advance())
				}
				for p.tok.kind == tokName {
					t.Interfaces = append(t.Interfaces, name())
					if p.isPunct("&") {
						must(p.advance())
					}
				}
			}
			directives()
			t.Fields = fields()
			add(t)
		case "union":
			t := &gqlSchemaType{Kind: kindUnion, Name: name(), Description: desc}
			directives()
			must(p.expectPunct("="))
			if p.isPunct("|") {
				must(p.advance())
			}
			for p.tok.kind == tokName {
				t.Possible = append(t.Possible, name())
				if !p.isPunct("|") {
					break
				}
				must(p.advance())
			}
			add(t)
		case "enum":
			t := &gqlSchemaType{Kind: kindEnum, Name: name(), Description: desc}
			directives()
			must(p.expectPunct("{"))
			for !p.isPunct("}") {
				ev := gqlEnumValue{Description: description()}
				ev.Name = name()
				directives()
				t.EnumValues = append(t.EnumValues, ev)
			}
			must(p.advance())
			add(t)
		case "input":
			t := &gqlSchemaType{Kind: kindInputObject, Name: name(), Description: desc}
			directives()
			t.InputFields = inputValues("{", "}")
			add(t)
		default:
			panic("graphql schema: unexpected definition " + keyword)
		}
	}
	// Interfaces learn which objects implement them.
	for _, n := range s.order {
		t := s.Types[n]
		if t.Kind != kindObject {
			continue
		}
		for _, iface := range t.Interfaces {
			it := s.Types[iface]
			if it == nil {
				panic("graphql schema: " + n + " implements unknown interface " + iface)
			}
			it.Possible = append(it.Possible, n)
		}
	}
	// Every named type a field or argument mentions must exist.
	check := func(owner string, ref *gqlTypeRef) {
		for ref.Of != nil {
			ref = ref.Of
		}
		if s.Types[ref.Name] == nil {
			panic("graphql schema: " + owner + " refers to unknown type " + ref.Name)
		}
	}
	for _, n := range s.order {
		t := s.Types[n]
		for _, f := range t.Fields {
			check(n+"."+f.Name, f.Type)
			for _, a := range f.Args {
				check(n+"."+f.Name+"("+a.Name+")", a.Type)
			}
		}
		for _, f := range t.InputFields {
			check(n+"."+f.Name, f.Type)
		}
	}
	return s
}

// ---------------------------------------------------------------- introspection

// The introspection types, in SDL like the rest of the schema.
const introspectionSDL = `
scalar String
scalar Int
scalar Float
scalar Boolean
scalar ID

type __Schema {
  description: String
  types: [__Type!]!
  queryType: __Type!
  mutationType: __Type
  subscriptionType: __Type
  directives: [__Directive!]!
}

type __Type {
  kind: __TypeKind!
  name: String
  description: String
  specifiedByURL: String
  fields(includeDeprecated: Boolean = false): [__Field!]
  interfaces: [__Type!]
  possibleTypes: [__Type!]
  enumValues(includeDeprecated: Boolean = false): [__EnumValue!]
  inputFields(includeDeprecated: Boolean = false): [__InputValue!]
  ofType: __Type
  isOneOf: Boolean
}

type __Field {
  name: String!
  description: String
  args(includeDeprecated: Boolean = false): [__InputValue!]!
  type: __Type!
  isDeprecated: Boolean!
  deprecationReason: String
}

type __InputValue {
  name: String!
  description: String
  type: __Type!
  defaultValue: String
  isDeprecated: Boolean!
  deprecationReason: String
}

type __EnumValue {
  name: String!
  description: String
  isDeprecated: Boolean!
  deprecationReason: String
}

enum __TypeKind {
  SCALAR
  OBJECT
  INTERFACE
  UNION
  ENUM
  INPUT_OBJECT
  LIST
  NON_NULL
}

type __Directive {
  name: String!
  description: String
  locations: [__DirectiveLocation!]!
  args(includeDeprecated: Boolean = false): [__InputValue!]!
  isRepeatable: Boolean!
}

enum __DirectiveLocation {
  QUERY
  MUTATION
  SUBSCRIPTION
  FIELD
  FRAGMENT_DEFINITION
  FRAGMENT_SPREAD
  INLINE_FRAGMENT
  VARIABLE_DEFINITION
  SCHEMA
  SCALAR
  OBJECT
  FIELD_DEFINITION
  ARGUMENT_DEFINITION
  INTERFACE
  UNION
  ENUM
  ENUM_VALUE
  INPUT_OBJECT
  INPUT_FIELD_DEFINITION
}
`

// introType is a __Type value: a named type, or a LIST / NON_NULL wrapper around one.
type introType struct {
	named   *gqlSchemaType
	wrapper string // kindList or kindNonNull, with of set
	of      *introType
}

type introDirective struct {
	name        string
	description string
	locations   []any
	args        []*gqlSchemaArg
}

func (s *gqlSchema) introRef(ref *gqlTypeRef) *introType {
	var t *introType
	if ref.Of != nil {
		t = &introType{wrapper: kindList, of: s.introRef(ref.Of)}
	} else {
		t = &introType{named: s.Types[ref.Name]}
	}
	if ref.NonNull {
		return &introType{wrapper: kindNonNull, of: t}
	}
	return t
}

func (s *gqlSchema) introNamed(name string) any {
	t := s.Types[name]
	if t == nil {
		return nil
	}
	return &introType{named: t}
}

// printValue writes a constant as GraphQL source, for __InputValue.defaultValue.
func printValue(v *gqlValue) string {
	switch v.Kind {
	case valString:
		return strconv.Quote(v.Text)
	case valList:
		out := "["
		for i, item := range v.List {
			if i > 0 {
				out += ", "
			}
			out += printValue(item)
		}
		return out + "]"
	case valObject:
		out := "{"
		for i, f := range v.Fields {
			if i > 0 {
				out += ", "
			}
			out += f.Name + ": " + printValue(f.Value)
		}
		return out + "}"
	case valNull:
		return "null"
	}
	return v.Text
}

// installIntrospection attaches the resolvers of the introspection types.
func (s *gqlSchema) installIntrospection() {
	s.attach("__Schema", "description", func(c *gqlExec, p any, a map[string]any) (any, error) { return nil, nil })
	s.attach("__Schema", "types", func(c *gqlExec, p any, a map[string]any) (any, error) {
		names := append([]string(nil), s.order...)
		sort.Strings(names)
		out := make([]any, 0, len(names))
		for _, n := range names {
			out = append(out, &introType{named: s.Types[n]})
		}
		return out, nil
	})
	s.attach("__Schema", "queryType", func(c *gqlExec, p any, a map[string]any) (any, error) { return s.introNamed(s.Query), nil })
	s.attach("__Schema", "mutationType", func(c *gqlExec, p any, a map[string]any) (any, error) { return s.introNamed(s.Mutation), nil })
	s.attach("__Schema", "subscriptionType", func(c *gqlExec, p any, a map[string]any) (any, error) {
		return s.introNamed(s.Subscription), nil
	})
	boolArg := &gqlSchemaArg{Name: "if", Type: &gqlTypeRef{Name: "Boolean", NonNull: true}}
	fieldLocations := []any{"FIELD", "FRAGMENT_SPREAD", "INLINE_FRAGMENT"}
	directives := []any{
		&introDirective{name: "include", description: "Directs the executor to include this field or fragment only when the `if` argument is true.", locations: fieldLocations, args: []*gqlSchemaArg{boolArg}},
		&introDirective{name: "skip", description: "Directs the executor to skip this field or fragment when the `if` argument is true.", locations: fieldLocations, args: []*gqlSchemaArg{boolArg}},
		&introDirective{name: "deprecated", description: "Marks an element of a GraphQL schema as no longer supported.", locations: []any{"FIELD_DEFINITION", "ENUM_VALUE", "ARGUMENT_DEFINITION", "INPUT_FIELD_DEFINITION"},
			args: []*gqlSchemaArg{{Name: "reason", Type: &gqlTypeRef{Name: "String"}, Default: &gqlValue{Kind: valString, Text: "No longer supported"}}}},
	}
	s.attach("__Schema", "directives", func(c *gqlExec, p any, a map[string]any) (any, error) { return directives, nil })

	s.attach("__Type", "kind", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper != "" {
			return t.wrapper, nil
		}
		return t.named.Kind, nil
	})
	s.attach("__Type", "name", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper != "" {
			return nil, nil
		}
		return t.named.Name, nil
	})
	s.attach("__Type", "description", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper != "" || t.named.Description == "" {
			return nil, nil
		}
		return t.named.Description, nil
	})
	s.attach("__Type", "specifiedByURL", func(c *gqlExec, p any, a map[string]any) (any, error) { return nil, nil })
	s.attach("__Type", "isOneOf", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper == "" && t.named.Kind == kindInputObject {
			return false, nil
		}
		return nil, nil
	})
	s.attach("__Type", "fields", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper != "" || (t.named.Kind != kindObject && t.named.Kind != kindInterface) {
			return nil, nil
		}
		includeDeprecated, _ := a["includeDeprecated"].(bool)
		out := make([]any, 0, len(t.named.Fields))
		for _, f := range t.named.Fields {
			if f.deprecated && !includeDeprecated {
				continue
			}
			out = append(out, f)
		}
		return out, nil
	})
	s.attach("__Type", "interfaces", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper != "" || (t.named.Kind != kindObject && t.named.Kind != kindInterface) {
			return nil, nil
		}
		out := make([]any, 0, len(t.named.Interfaces))
		for _, n := range t.named.Interfaces {
			out = append(out, &introType{named: s.Types[n]})
		}
		return out, nil
	})
	s.attach("__Type", "possibleTypes", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper != "" || (t.named.Kind != kindInterface && t.named.Kind != kindUnion) {
			return nil, nil
		}
		out := make([]any, 0, len(t.named.Possible))
		for _, n := range t.named.Possible {
			out = append(out, &introType{named: s.Types[n]})
		}
		return out, nil
	})
	s.attach("__Type", "enumValues", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper != "" || t.named.Kind != kindEnum {
			return nil, nil
		}
		out := make([]any, 0, len(t.named.EnumValues))
		for i := range t.named.EnumValues {
			out = append(out, &t.named.EnumValues[i])
		}
		return out, nil
	})
	s.attach("__Type", "inputFields", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.wrapper != "" || t.named.Kind != kindInputObject {
			return nil, nil
		}
		out := make([]any, 0, len(t.named.InputFields))
		for _, f := range t.named.InputFields {
			out = append(out, f)
		}
		return out, nil
	})
	s.attach("__Type", "ofType", func(c *gqlExec, p any, a map[string]any) (any, error) {
		t := p.(*introType)
		if t.of == nil {
			return nil, nil
		}
		return t.of, nil
	})

	nullable := func(text string) any {
		if text == "" {
			return nil
		}
		return text
	}
	s.attach("__Field", "name", func(c *gqlExec, p any, a map[string]any) (any, error) { return p.(*gqlSchemaField).Name, nil })
	s.attach("__Field", "description", func(c *gqlExec, p any, a map[string]any) (any, error) {
		return nullable(p.(*gqlSchemaField).Description), nil
	})
	s.attach("__Field", "args", func(c *gqlExec, p any, a map[string]any) (any, error) {
		f := p.(*gqlSchemaField)
		out := make([]any, 0, len(f.Args))
		for _, arg := range f.Args {
			out = append(out, arg)
		}
		return out, nil
	})
	s.attach("__Field", "type", func(c *gqlExec, p any, a map[string]any) (any, error) {
		return s.introRef(p.(*gqlSchemaField).Type), nil
	})
	s.attach("__Field", "isDeprecated", func(c *gqlExec, p any, a map[string]any) (any, error) { return p.(*gqlSchemaField).deprecated, nil })
	s.attach("__Field", "deprecationReason", func(c *gqlExec, p any, a map[string]any) (any, error) {
		f := p.(*gqlSchemaField)
		if !f.deprecated {
			return nil, nil
		}
		return f.Deprecation, nil
	})

	s.attach("__InputValue", "name", func(c *gqlExec, p any, a map[string]any) (any, error) { return p.(*gqlSchemaArg).Name, nil })
	s.attach("__InputValue", "description", func(c *gqlExec, p any, a map[string]any) (any, error) {
		return nullable(p.(*gqlSchemaArg).Description), nil
	})
	s.attach("__InputValue", "type", func(c *gqlExec, p any, a map[string]any) (any, error) { return s.introRef(p.(*gqlSchemaArg).Type), nil })
	s.attach("__InputValue", "defaultValue", func(c *gqlExec, p any, a map[string]any) (any, error) {
		arg := p.(*gqlSchemaArg)
		if arg.Default == nil {
			return nil, nil
		}
		return printValue(arg.Default), nil
	})
	s.attach("__InputValue", "isDeprecated", func(c *gqlExec, p any, a map[string]any) (any, error) { return false, nil })
	s.attach("__InputValue", "deprecationReason", func(c *gqlExec, p any, a map[string]any) (any, error) { return nil, nil })

	s.attach("__EnumValue", "name", func(c *gqlExec, p any, a map[string]any) (any, error) { return p.(*gqlEnumValue).Name, nil })
	s.attach("__EnumValue", "description", func(c *gqlExec, p any, a map[string]any) (any, error) {
		return nullable(p.(*gqlEnumValue).Description), nil
	})
	s.attach("__EnumValue", "isDeprecated", func(c *gqlExec, p any, a map[string]any) (any, error) { return false, nil })
	s.attach("__EnumValue", "deprecationReason", func(c *gqlExec, p any, a map[string]any) (any, error) { return nil, nil })

	s.attach("__Directive", "name", func(c *gqlExec, p any, a map[string]any) (any, error) { return p.(*introDirective).name, nil })
	s.attach("__Directive", "description", func(c *gqlExec, p any, a map[string]any) (any, error) {
		return nullable(p.(*introDirective).description), nil
	})
	s.attach("__Directive", "locations", func(c *gqlExec, p any, a map[string]any) (any, error) { return p.(*introDirective).locations, nil })
	s.attach("__Directive", "args", func(c *gqlExec, p any, a map[string]any) (any, error) {
		d := p.(*introDirective)
		out := make([]any, 0, len(d.args))
		for _, arg := range d.args {
			out = append(out, arg)
		}
		return out, nil
	})
	s.attach("__Directive", "isRepeatable", func(c *gqlExec, p any, a map[string]any) (any, error) { return false, nil })
}
