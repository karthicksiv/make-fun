package main

// GraphQL validation and execution over a gqlSchema: variable and argument coercion, fragments,
// @include/@skip, non-null propagation, and errors shaped the way GitHub's API shapes them.

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// gqlError is one entry of a response's errors array.
type gqlError struct {
	Message    string
	Type       string // GitHub's top-level "type" (NOT_FOUND, FORBIDDEN, UNPROCESSABLE ...), when set
	Path       []any
	Line       int
	Col        int
	Extensions *orderedMap
}

func (e *gqlError) Error() string { return e.Message }

// orderedMap is a JSON object whose keys keep their insertion order.
type orderedMap struct {
	keys []string
	vals []any
}

func (m *orderedMap) set(key string, value any) *orderedMap {
	for i, k := range m.keys {
		if k == key {
			m.vals[i] = value
			return m
		}
	}
	m.keys = append(m.keys, key)
	m.vals = append(m.vals, value)
	return m
}

func (m *orderedMap) get(key string) (any, bool) {
	for i, k := range m.keys {
		if k == key {
			return m.vals[i], true
		}
	}
	return nil, false
}

// om builds an orderedMap from alternating keys and values.
func om(pairs ...any) *orderedMap {
	m := &orderedMap{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m.keys = append(m.keys, pairs[i].(string))
		m.vals = append(m.vals, pairs[i+1])
	}
	return m
}

// gqlNamed lets a resolved value say which object type it is, for interfaces and unions.
type gqlNamed interface {
	gqlTypeName() string
}

// gqlFielder lets a resolved value answer fields that have no resolver attached.
type gqlFielder interface {
	gqlField(name string) (any, bool)
}

// gqlExec is one operation's execution.
type gqlExec struct {
	schema *gqlSchema
	doc    *gqlDocument
	vars   map[string]any
	errors []*gqlError
	app    *App
	// base is the origin the client dialed, for the URLs objects carry.
	base string
}

type gqlResult struct {
	Data    *orderedMap
	HasData bool
	Errors  []*gqlError
}

// toJSON renders the result as a GraphQL response body.
func (r *gqlResult) toJSON() []byte {
	out := &orderedMap{}
	if r.HasData {
		if r.Data == nil {
			out.set("data", nil)
		} else {
			out.set("data", r.Data)
		}
	}
	if len(r.Errors) > 0 {
		list := make([]any, 0, len(r.Errors))
		for _, e := range r.Errors {
			item := &orderedMap{}
			if e.Type != "" {
				item.set("type", e.Type)
			}
			if e.Path != nil {
				item.set("path", e.Path)
			}
			if e.Extensions != nil {
				item.set("extensions", e.Extensions)
			}
			if e.Line > 0 {
				item.set("locations", []any{om("line", e.Line, "column", e.Col)})
			}
			item.set("message", e.Message)
			list = append(list, item)
		}
		out.set("errors", list)
	}
	return appendJSON(make([]byte, 0, 512), out)
}

// runGraphQL parses, validates and executes one request.
func runGraphQL(app *App, schema *gqlSchema, base string, query string, operationName string, variables map[string]any) *gqlResult {
	doc, syntaxErr := parseGraphQL(query)
	if syntaxErr != nil {
		return &gqlResult{Errors: []*gqlError{{Message: syntaxErr.Message, Line: syntaxErr.Line, Col: syntaxErr.Col}}}
	}
	if len(doc.Operations) == 0 {
		return &gqlResult{Errors: []*gqlError{{Message: "A query attribute must be specified and must be a string."}}}
	}
	var op *gqlOperation
	if operationName != "" {
		for _, candidate := range doc.Operations {
			if candidate.Name == operationName {
				op = candidate
			}
		}
		if op == nil {
			return &gqlResult{Errors: []*gqlError{{Message: fmt.Sprintf("No operation named %q", operationName)}}}
		}
	} else if len(doc.Operations) == 1 {
		op = doc.Operations[0]
	} else {
		return &gqlResult{Errors: []*gqlError{{Message: "An operation name is required"}}}
	}
	return runOperation(app, schema, base, doc, op, variables)
}

func runOperation(app *App, schema *gqlSchema, base string, doc *gqlDocument, op *gqlOperation, variables map[string]any) *gqlResult {
	c := &gqlExec{schema: schema, doc: doc, vars: map[string]any{}, app: app, base: base}
	rootName := schema.Query
	switch op.Type {
	case "mutation":
		rootName = schema.Mutation
	case "subscription":
		rootName = schema.Subscription
	}
	root := schema.typ(rootName)
	if root == nil {
		return &gqlResult{Errors: []*gqlError{{Message: fmt.Sprintf("Schema is not configured for %ss", op.Type), Line: op.Line, Col: op.Col}}}
	}

	// Validation: every field, argument, fragment and variable must make sense before anything runs.
	v := &gqlValidator{schema: schema, doc: doc, op: op, declared: map[string]*gqlVariableDef{}}
	for i := range op.Variables {
		v.declared[op.Variables[i].Name] = &op.Variables[i]
	}
	opLabel := op.Type
	if op.Name != "" {
		opLabel += " " + op.Name
	}
	v.selections(root, op.Selections, []any{opLabel}, map[string]bool{})
	if len(v.errors) > 0 {
		return &gqlResult{Errors: v.errors}
	}

	// Variables: what the request gave, else the declared default; a missing non-null one is an error.
	for i := range op.Variables {
		def := &op.Variables[i]
		given, present := variables[def.Name]
		switch {
		case present:
			coerced, err := c.coerceInput(def.Type, given, "$"+def.Name)
			if err != nil {
				c.errors = append(c.errors, &gqlError{
					Message: fmt.Sprintf("Variable $%s of type %s was provided invalid value", def.Name, def.Type.String()),
					Line:    op.Line, Col: op.Col,
					Extensions: om("value", given, "problems", []any{om("path", []any{}, "explanation", err.Error())}),
				})
				continue
			}
			c.vars[def.Name] = coerced
		case def.Default != nil:
			value, err := c.coerceInput(def.Type, c.literal(def.Default), "$"+def.Name)
			if err == nil {
				c.vars[def.Name] = value
			}
		case def.Type.NonNull:
			c.errors = append(c.errors, &gqlError{
				Message: fmt.Sprintf("Variable $%s of type %s was provided invalid value", def.Name, def.Type.String()),
				Line:    op.Line, Col: op.Col,
				Extensions: om("value", nil, "problems", []any{om("path", []any{}, "explanation", "Expected value to not be null")}),
			})
		}
	}
	if len(c.errors) > 0 {
		return &gqlResult{Errors: c.errors}
	}

	data, ok := c.selectionSet(root, struct{}{}, op.Selections, nil)
	res := &gqlResult{HasData: true, Errors: c.errors}
	if ok {
		res.Data = data
	}
	return res
}

// ---------------------------------------------------------------- validation

type gqlValidator struct {
	schema   *gqlSchema
	doc      *gqlDocument
	op       *gqlOperation
	declared map[string]*gqlVariableDef
	errors   []*gqlError
}

func (v *gqlValidator) add(sel *gqlSelection, path []any, code string, message string, extra ...any) {
	ext := om("code", code)
	for i := 0; i+1 < len(extra); i += 2 {
		ext.set(extra[i].(string), extra[i+1])
	}
	e := &gqlError{Message: message, Path: append([]any(nil), path...), Extensions: ext}
	if sel != nil {
		e.Line, e.Col = sel.Line, sel.Col
	}
	v.errors = append(v.errors, e)
}

func (v *gqlValidator) checkVariables(value *gqlValue, sel *gqlSelection, path []any) {
	switch value.Kind {
	case valVariable:
		if _, ok := v.declared[value.Text]; !ok {
			label := "anonymous query"
			if v.op.Name != "" {
				label = v.op.Type + " " + v.op.Name
			}
			v.add(sel, path, "variableNotDefined", fmt.Sprintf("Variable $%s is used by %s but not declared", value.Text, label), "variableName", value.Text)
		}
	case valList:
		for _, item := range value.List {
			v.checkVariables(item, sel, path)
		}
	case valObject:
		for _, f := range value.Fields {
			v.checkVariables(f.Value, sel, path)
		}
	}
}

func (v *gqlValidator) selections(parent *gqlSchemaType, sels []*gqlSelection, path []any, spreading map[string]bool) {
	for _, sel := range sels {
		for _, d := range sel.Directives {
			for _, a := range d.Args {
				v.checkVariables(a.Value, sel, path)
			}
		}
		switch {
		case sel.Field != nil:
			f := sel.Field
			fieldPath := append(append([]any(nil), path...), f.ResponseKey())
			if f.Name == "__typename" {
				continue
			}
			var def *gqlSchemaField
			if parent.Name == v.schema.Query && (f.Name == "__schema" || f.Name == "__type") {
				def = metaField(f.Name)
			} else {
				def = parent.field(f.Name)
			}
			if def == nil {
				v.add(sel, fieldPath, "undefinedField", fmt.Sprintf("Field '%s' doesn't exist on type '%s'", f.Name, parent.Name), "typeName", parent.Name, "fieldName", f.Name)
				continue
			}
			given := map[string]bool{}
			for _, a := range f.Args {
				given[a.Name] = true
				if def.arg(a.Name) == nil {
					v.add(sel, append(append([]any(nil), fieldPath...), a.Name), "argumentNotAccepted",
						fmt.Sprintf("Field '%s' doesn't accept argument '%s'", f.Name, a.Name), "name", f.Name, "typeName", "Field", "argumentName", a.Name)
					continue
				}
				v.checkVariables(a.Value, sel, fieldPath)
			}
			var missing []string
			for _, a := range def.Args {
				if a.Type.NonNull && a.Default == nil && !given[a.Name] {
					missing = append(missing, a.Name)
				}
			}
			if len(missing) > 0 {
				v.add(sel, fieldPath, "missingRequiredArguments",
					fmt.Sprintf("Field '%s' is missing required arguments: %s", f.Name, strings.Join(missing, ", ")), "className", "Field", "name", f.Name, "arguments", strings.Join(missing, ", "))
			}
			named := def.Type
			for named.Of != nil {
				named = named.Of
			}
			target := v.schema.typ(named.Name)
			if target.isComposite() {
				if len(f.Selections) == 0 {
					v.add(sel, fieldPath, "selectionMismatch",
						fmt.Sprintf("Field must have selections (field '%s' returns %s but has no selections. Did you mean '%s { ... }'?)", f.Name, named.Name, f.Name), "nodeName", "field '"+f.Name+"'", "typeName", named.Name)
					continue
				}
				v.selections(target, f.Selections, fieldPath, spreading)
			} else if len(f.Selections) > 0 {
				v.add(sel, fieldPath, "selectionMismatch",
					fmt.Sprintf("Selections can't be made on scalars (field '%s' returns %s but has selections [%s])", f.Name, named.Name, selectionNames(f.Selections)), "nodeName", "field '"+f.Name+"'", "typeName", named.Name)
			}
		case sel.InlineFragment != nil:
			target := parent
			if cond := sel.InlineFragment.TypeCondition; cond != "" {
				target = v.schema.typ(cond)
				if target == nil {
					v.add(sel, path, "undefinedType", fmt.Sprintf("No such type %s, so it can't be a fragment condition", cond), "typeName", cond)
					continue
				}
			}
			label := "..."
			if target != parent {
				label = "... on " + target.Name
			}
			v.selections(target, sel.InlineFragment.Selections, append(append([]any(nil), path...), label), spreading)
		default:
			frag := v.doc.Fragments[sel.FragmentSpread]
			if frag == nil {
				v.add(sel, append(append([]any(nil), path...), "... "+sel.FragmentSpread), "undefinedFragment",
					fmt.Sprintf("Fragment %s was used, but not defined", sel.FragmentSpread), "fragmentName", sel.FragmentSpread)
				continue
			}
			if spreading[frag.Name] {
				continue
			}
			target := v.schema.typ(frag.TypeCondition)
			if target == nil {
				v.add(sel, path, "undefinedType", fmt.Sprintf("No such type %s, so it can't be a fragment condition", frag.TypeCondition), "typeName", frag.TypeCondition)
				continue
			}
			spreading[frag.Name] = true
			v.selections(target, frag.Selections, []any{"fragment " + frag.Name}, spreading)
			delete(spreading, frag.Name)
		}
	}
}

func selectionNames(sels []*gqlSelection) string {
	var names []string
	for _, s := range sels {
		if s.Field != nil {
			names = append(names, strconv.Quote(s.Field.Name))
		}
	}
	return strings.Join(names, ", ")
}

var metaSchemaField = &gqlSchemaField{Name: "__schema", Type: &gqlTypeRef{Name: "__Schema", NonNull: true}}
var metaTypeField = &gqlSchemaField{
	Name: "__type", Type: &gqlTypeRef{Name: "__Type"},
	Args: []*gqlSchemaArg{{Name: "name", Type: &gqlTypeRef{Name: "String", NonNull: true}}},
}

func metaField(name string) *gqlSchemaField {
	if name == "__schema" {
		return metaSchemaField
	}
	return metaTypeField
}

// ---------------------------------------------------------------- input coercion

// literal turns a value literal into plain Go data, variables substituted.
func (c *gqlExec) literal(v *gqlValue) any {
	switch v.Kind {
	case valVariable:
		return c.vars[v.Text]
	case valInt:
		n, err := strconv.ParseInt(v.Text, 10, 64)
		if err != nil {
			f, _ := strconv.ParseFloat(v.Text, 64)
			return f
		}
		return int(n)
	case valFloat:
		f, _ := strconv.ParseFloat(v.Text, 64)
		return f
	case valString:
		return v.Text
	case valBoolean:
		return v.Text == "true"
	case valNull:
		return nil
	case valEnum:
		return gqlEnumLiteral(v.Text)
	case valList:
		out := make([]any, len(v.List))
		for i, item := range v.List {
			out[i] = c.literal(item)
		}
		return out
	case valObject:
		out := make(map[string]any, len(v.Fields))
		for _, f := range v.Fields {
			if f.Value.Kind == valVariable {
				if _, ok := c.vars[f.Value.Text]; !ok {
					continue
				}
			}
			out[f.Name] = c.literal(f.Value)
		}
		return out
	}
	return nil
}

// gqlEnumLiteral marks a value written as a bare enum name in the document.
type gqlEnumLiteral string

// coerceInput checks and converts an input value against its declared type.
func (c *gqlExec) coerceInput(t *gqlTypeRef, value any, where string) (any, error) {
	if value == nil {
		if t.NonNull {
			return nil, fmt.Errorf("Expected value to not be null")
		}
		return nil, nil
	}
	if t.Of != nil {
		list, ok := value.([]any)
		if !ok {
			item, err := c.coerceInput(t.Of, value, where)
			if err != nil {
				return nil, err
			}
			return []any{item}, nil
		}
		out := make([]any, len(list))
		for i, item := range list {
			coerced, err := c.coerceInput(t.Of, item, where)
			if err != nil {
				return nil, err
			}
			out[i] = coerced
		}
		return out, nil
	}
	named := c.schema.typ(t.Name)
	if named == nil {
		return nil, fmt.Errorf("%s isn't a defined input type", t.Name)
	}
	switch named.Kind {
	case kindScalar:
		if enum, ok := value.(gqlEnumLiteral); ok {
			value = string(enum)
		}
		switch t.Name {
		case "Int":
			switch n := value.(type) {
			case int:
				return n, nil
			case int64:
				return int(n), nil
			case float64:
				if n == math.Trunc(n) && math.Abs(n) < 1<<31 {
					return int(n), nil
				}
			}
			return nil, fmt.Errorf("Could not coerce value %v to Int", jsonText(value))
		case "Float":
			switch n := value.(type) {
			case int:
				return float64(n), nil
			case float64:
				return n, nil
			}
			return nil, fmt.Errorf("Could not coerce value %v to Float", jsonText(value))
		case "Boolean":
			if b, ok := value.(bool); ok {
				return b, nil
			}
			return nil, fmt.Errorf("Could not coerce value %v to Boolean", jsonText(value))
		case "String":
			if s, ok := value.(string); ok {
				return s, nil
			}
			return nil, fmt.Errorf("Could not coerce value %v to String", jsonText(value))
		case "ID":
			switch id := value.(type) {
			case string:
				return id, nil
			case int:
				return strconv.Itoa(id), nil
			case float64:
				if id == math.Trunc(id) {
					return strconv.FormatInt(int64(id), 10), nil
				}
			}
			return nil, fmt.Errorf("Could not coerce value %v to ID", jsonText(value))
		}
		// A custom scalar (DateTime, URI, GitObjectID ...) is taken as written.
		return value, nil
	case kindEnum:
		var text string
		switch e := value.(type) {
		case gqlEnumLiteral:
			text = string(e)
		case string:
			text = e
		default:
			return nil, fmt.Errorf("Expected %v to be one of: %s", jsonText(value), enumNames(named))
		}
		for _, ev := range named.EnumValues {
			if ev.Name == text {
				return text, nil
			}
		}
		return nil, fmt.Errorf("Expected %q to be one of: %s", text, enumNames(named))
	case kindInputObject:
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Expected %v to be a key-value object", jsonText(value))
		}
		out := make(map[string]any, len(obj))
		for key := range obj {
			if named.inputField(key) == nil {
				return nil, fmt.Errorf("Field is not defined on %s", named.Name)
			}
		}
		for _, f := range named.InputFields {
			given, present := obj[f.Name]
			if !present {
				if f.Default != nil {
					def, err := c.coerceInput(f.Type, c.literal(f.Default), where)
					if err == nil {
						out[f.Name] = def
					}
				} else if f.Type.NonNull {
					return nil, fmt.Errorf("Expected value to not be null")
				}
				continue
			}
			coerced, err := c.coerceInput(f.Type, given, where)
			if err != nil {
				return nil, err
			}
			out[f.Name] = coerced
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s isn't a valid input type", t.Name)
}

func enumNames(t *gqlSchemaType) string {
	names := make([]string, len(t.EnumValues))
	for i, ev := range t.EnumValues {
		names[i] = ev.Name
	}
	return strings.Join(names, ", ")
}

func jsonText(v any) string { return string(appendJSON(nil, v)) }

// arguments computes a field's argument map: given values coerced, defaults filled in.
func (c *gqlExec) arguments(def *gqlSchemaField, node *gqlFieldNode, sel *gqlSelection, path []any) (map[string]any, bool) {
	args := make(map[string]any, len(def.Args))
	for _, argDef := range def.Args {
		var given *gqlValue
		for i := range node.Args {
			if node.Args[i].Name == argDef.Name {
				given = node.Args[i].Value
			}
		}
		if given != nil && given.Kind == valVariable {
			if _, ok := c.vars[given.Text]; !ok {
				given = nil
			}
		}
		if given == nil {
			if argDef.Default != nil {
				if value, err := c.coerceInput(argDef.Type, c.literal(argDef.Default), argDef.Name); err == nil {
					args[argDef.Name] = value
				}
			} else if argDef.Type.NonNull {
				c.errors = append(c.errors, &gqlError{
					Message: fmt.Sprintf("Argument '%s' on Field '%s' has an invalid value (null). Expected type '%s'.", argDef.Name, node.Name, argDef.Type.String()),
					Path:    append([]any(nil), path...), Line: sel.Line, Col: sel.Col,
					Extensions: om("code", "argumentLiteralsIncompatible", "typeName", "Field", "argumentName", argDef.Name),
				})
				return nil, false
			}
			continue
		}
		value, err := c.coerceInput(argDef.Type, c.literal(given), argDef.Name)
		if err != nil {
			c.errors = append(c.errors, &gqlError{
				Message: fmt.Sprintf("Argument '%s' on Field '%s' has an invalid value (%s). Expected type '%s'.", argDef.Name, node.Name, printValue(given), argDef.Type.String()),
				Path:    append([]any(nil), path...), Line: sel.Line, Col: sel.Col,
				Extensions: om("code", "argumentLiteralsIncompatible", "typeName", "Field", "argumentName", argDef.Name),
			})
			return nil, false
		}
		args[argDef.Name] = value
	}
	return args, true
}

// ---------------------------------------------------------------- execution

type collectedField struct {
	key   string
	nodes []*gqlFieldNode
	sel   *gqlSelection
}

func (c *gqlExec) included(dirs []gqlDirective) bool {
	for _, d := range dirs {
		if d.Name != "skip" && d.Name != "include" {
			continue
		}
		for _, a := range d.Args {
			if a.Name != "if" {
				continue
			}
			flag, _ := c.literal(a.Value).(bool)
			if d.Name == "skip" && flag {
				return false
			}
			if d.Name == "include" && !flag {
				return false
			}
		}
	}
	return true
}

func (c *gqlExec) collect(objectType string, sels []*gqlSelection, out *[]*collectedField, seen map[string]bool) {
	for _, sel := range sels {
		if !c.included(sel.Directives) {
			continue
		}
		switch {
		case sel.Field != nil:
			key := sel.Field.ResponseKey()
			merged := false
			for _, existing := range *out {
				if existing.key == key {
					existing.nodes = append(existing.nodes, sel.Field)
					merged = true
					break
				}
			}
			if !merged {
				*out = append(*out, &collectedField{key: key, nodes: []*gqlFieldNode{sel.Field}, sel: sel})
			}
		case sel.InlineFragment != nil:
			cond := sel.InlineFragment.TypeCondition
			if cond == "" || c.schema.possibleType(cond, objectType) {
				c.collect(objectType, sel.InlineFragment.Selections, out, seen)
			}
		default:
			if seen[sel.FragmentSpread] {
				continue
			}
			seen[sel.FragmentSpread] = true
			frag := c.doc.Fragments[sel.FragmentSpread]
			if frag != nil && c.schema.possibleType(frag.TypeCondition, objectType) {
				c.collect(objectType, frag.Selections, out, seen)
			}
		}
	}
}

// selectionSet executes selections against one object. ok is false when a non-null field under it
// came back null, which nulls the object itself.
func (c *gqlExec) selectionSet(objectType *gqlSchemaType, source any, sels []*gqlSelection, path []any) (*orderedMap, bool) {
	var fields []*collectedField
	c.collect(objectType.Name, sels, &fields, map[string]bool{})
	out := &orderedMap{keys: make([]string, 0, len(fields)), vals: make([]any, 0, len(fields))}
	for _, cf := range fields {
		node := cf.nodes[0]
		fieldPath := append(append(make([]any, 0, len(path)+1), path...), cf.key)
		if node.Name == "__typename" {
			out.set(cf.key, objectType.Name)
			continue
		}
		var def *gqlSchemaField
		var value any
		var err error
		switch {
		case objectType.Name == c.schema.Query && node.Name == "__schema":
			def = metaSchemaField
			value = struct{}{}
		case objectType.Name == c.schema.Query && node.Name == "__type":
			def = metaTypeField
			args, ok := c.arguments(def, node, cf.sel, fieldPath)
			if !ok {
				out.set(cf.key, nil)
				continue
			}
			name, _ := args["name"].(string)
			value = c.schema.introNamed(name)
		default:
			def = objectType.field(node.Name)
			if def == nil {
				continue
			}
			args, ok := c.arguments(def, node, cf.sel, fieldPath)
			if !ok {
				if def.Type.NonNull {
					return nil, false
				}
				out.set(cf.key, nil)
				continue
			}
			if def.Resolve != nil {
				value, err = def.Resolve(c, source, args)
			} else {
				value = defaultResolve(source, node.Name)
			}
		}
		if err != nil {
			ge, isGQL := err.(*gqlError)
			if !isGQL {
				ge = &gqlError{Message: err.Error()}
			} else {
				copied := *ge
				ge = &copied
			}
			ge.Path = fieldPath
			if ge.Line == 0 {
				ge.Line, ge.Col = cf.sel.Line, cf.sel.Col
			}
			c.errors = append(c.errors, ge)
			if def.Type.NonNull {
				return nil, false
			}
			out.set(cf.key, nil)
			continue
		}
		var subSelections []*gqlSelection
		if len(cf.nodes) == 1 {
			subSelections = node.Selections
		} else {
			for _, n := range cf.nodes {
				subSelections = append(subSelections, n.Selections...)
			}
		}
		completed, ok := c.complete(def.Type, value, subSelections, fieldPath, cf.sel)
		if !ok {
			return nil, false
		}
		out.set(cf.key, completed)
	}
	return out, true
}

func defaultResolve(source any, name string) any {
	switch s := source.(type) {
	case map[string]any:
		return s[name]
	case *orderedMap:
		v, _ := s.get(name)
		return v
	case gqlFielder:
		v, _ := s.gqlField(name)
		return v
	}
	return nil
}

// isNil reports a nil interface or a typed nil pointer, map or slice inside one.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Interface, reflect.Func:
		return rv.IsNil()
	}
	return false
}

// complete turns a resolved value into response data for its declared type.
func (c *gqlExec) complete(t *gqlTypeRef, value any, sels []*gqlSelection, path []any, sel *gqlSelection) (any, bool) {
	if isNil(value) {
		if t.NonNull {
			if len(c.errors) == 0 || !samePath(c.errors[len(c.errors)-1].Path, path) {
				c.errors = append(c.errors, &gqlError{Message: "Cannot return null for non-nullable field", Path: path, Line: sel.Line, Col: sel.Col})
			}
			return nil, false
		}
		return nil, true
	}
	if t.Of != nil {
		list, ok := value.([]any)
		if !ok {
			rv := reflect.ValueOf(value)
			if rv.Kind() != reflect.Slice {
				return nil, !t.NonNull
			}
			list = make([]any, rv.Len())
			for i := range list {
				list[i] = rv.Index(i).Interface()
			}
		}
		out := make([]any, len(list))
		for i, item := range list {
			itemPath := append(append(make([]any, 0, len(path)+1), path...), i)
			completed, ok := c.complete(t.Of, item, sels, itemPath, sel)
			if !ok {
				return nil, !t.NonNull
			}
			out[i] = completed
		}
		return out, true
	}
	named := c.schema.typ(t.Name)
	switch named.Kind {
	case kindScalar, kindEnum:
		if enum, ok := value.(gqlEnumLiteral); ok {
			return string(enum), true
		}
		return value, true
	case kindObject:
		data, ok := c.selectionSet(named, value, sels, path)
		if !ok {
			return nil, !t.NonNull
		}
		return data, true
	default: // interface or union: the value says which object it is
		var concrete *gqlSchemaType
		switch v := value.(type) {
		case gqlNamed:
			concrete = c.schema.typ(v.gqlTypeName())
		case map[string]any:
			if name, ok := v["__typename"].(string); ok {
				concrete = c.schema.typ(name)
			}
		}
		if concrete == nil || !c.schema.possibleType(named.Name, concrete.Name) {
			return nil, !t.NonNull
		}
		data, ok := c.selectionSet(concrete, value, sels, path)
		if !ok {
			return nil, !t.NonNull
		}
		return data, true
	}
}

func samePath(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- JSON

// appendJSON writes plain Go data (nil, bool, numbers, strings, []any, map[string]any, orderedMap)
// as JSON. Map keys of a plain map are written sorted, so output is stable.
func appendJSON(dst []byte, v any) []byte {
	switch x := v.(type) {
	case nil:
		return append(dst, "null"...)
	case bool:
		if x {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	case string:
		return appendJSONString(dst, x)
	case gqlEnumLiteral:
		return appendJSONString(dst, string(x))
	case int:
		return strconv.AppendInt(dst, int64(x), 10)
	case int64:
		return strconv.AppendInt(dst, x, 10)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.AppendInt(dst, int64(x), 10)
		}
		return strconv.AppendFloat(dst, x, 'g', -1, 64)
	case *orderedMap:
		if x == nil {
			return append(dst, "null"...)
		}
		dst = append(dst, '{')
		for i, k := range x.keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSONString(dst, k)
			dst = append(dst, ':')
			dst = appendJSON(dst, x.vals[i])
		}
		return append(dst, '}')
	case []any:
		dst = append(dst, '[')
		for i, item := range x {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSON(dst, item)
		}
		return append(dst, ']')
	case []string:
		dst = append(dst, '[')
		for i, item := range x {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSONString(dst, item)
		}
		return append(dst, ']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		dst = append(dst, '{')
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSONString(dst, k)
			dst = append(dst, ':')
			dst = appendJSON(dst, x[k])
		}
		return append(dst, '}')
	case rawJSON:
		return append(dst, x...)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice {
		dst = append(dst, '[')
		for i := 0; i < rv.Len(); i++ {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSON(dst, rv.Index(i).Interface())
		}
		return append(dst, ']')
	}
	return appendJSONString(dst, fmt.Sprint(v))
}

// rawJSON is JSON text spliced into output as it stands.
type rawJSON []byte

// appendJSONString writes a JSON string literal.
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		dst = append(dst, s[start:i]...)
		switch c {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&15])
		}
		start = i + 1
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}
