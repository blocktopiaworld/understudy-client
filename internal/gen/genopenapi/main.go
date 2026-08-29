// Command genopenapi writes docs/openapi.yaml from the control server's own
// route table.
//
//	go run ./internal/gen/genopenapi > docs/openapi.yaml
//
// # Why this is generated
//
// The prose reference was written by reading the handlers, and four of its
// claims were wrong: a single-block dig carries no "dug", a deposit under
// "all" answers with "stacks", brewing returns no item, and the library example
// named a field that did not compile. Prose drifts from code silently, and a
// hand-maintained OpenAPI document is prose with a schema attached — it would
// drift the same way and be believed harder.
//
// So this reads routes.go itself: the paths and methods from the mux calls, the
// request fields and their types from each handler's input struct, and the
// prose from the doc comment already sitting above it. What the spec says is
// therefore what the server accepts, by construction.
//
// # What it cannot see
//
// Response bodies. A handler builds them as body{...} literals whose values are
// expressions, and following those to their types means type-checking the whole
// package for a payoff of about forty field types. The envelope is described
// properly because it is the same everywhere; the per-verb keys are listed by
// name with the guide linked for what they mean. Where that is not enough, the
// guide carries a captured example of every endpoint.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const source = "internal/control/routes.go"

type field struct {
	name     string // the JSON name
	kind     string // OpenAPI type
	items    string // element type, for arrays
	optional bool   // a pointer, so absence is distinguishable from zero
	comment  string
}

type route struct {
	method  string
	path    string
	handler string
	summary string
	desc    string
	body    []field
	query   []field
}

func main() {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, source, nil, parser.ParseComments)
	if err != nil {
		fail("parsing %s: %v", source, err)
	}

	handlers := map[string]*ast.FuncDecl{}
	types := map[string]*ast.StructType{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil && len(d.Recv.List) == 1 {
				handlers[d.Name.Name] = d
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					types[ts.Name.Name] = st
				}
			}
		}
	}

	routes := collect(file, handlers, types)
	if len(routes) == 0 {
		fail("no routes found in %s — has the mux registration changed shape?", source)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].path != routes[j].path {
			return routes[i].path < routes[j].path
		}
		return routes[i].method < routes[j].method
	})
	fmt.Print(render(routes))
	fmt.Fprintf(os.Stderr, "genopenapi: %d paths written\n", len(routes))
}

var (
	reHandle = regexp.MustCompile(`^"(GET|POST) ([^"]+)"$`)
	reJSON   = regexp.MustCompile(`json:"([^",]+)`)
	reQuery  = regexp.MustCompile(`Query\(\)\.(?:Get|Has)\("([a-z_]+)"\)`)
)

// collect walks the mux registrations and pairs each with its handler.
func collect(file *ast.File, handlers map[string]*ast.FuncDecl, types map[string]*ast.StructType) []route {
	var out []route
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		m := reHandle.FindStringSubmatch(lit.Value)
		if m == nil {
			return true
		}
		name := handlerName(call.Args[1])
		if name == "" {
			return true
		}
		r := route{method: m[1], path: m[2], handler: name}
		if fn, ok := handlers[name]; ok {
			r.summary, r.desc = docOf(fn)
			r.body, r.query = fieldsOf(fn, types)
		}
		out = append(out, r)
		return true
	})
	return out
}

// handlerName pulls s.foo out of either handle(s, s.foo) or a bare s.foo.
func handlerName(arg ast.Expr) string {
	switch a := arg.(type) {
	case *ast.SelectorExpr:
		return a.Sel.Name
	case *ast.CallExpr:
		for _, inner := range a.Args {
			if sel, ok := inner.(*ast.SelectorExpr); ok {
				return sel.Sel.Name
			}
		}
	}
	return ""
}

// docOf splits a handler's doc comment into a first line and the rest, which is
// how OpenAPI wants summary and description.
func docOf(fn *ast.FuncDecl) (summary, description string) {
	if fn.Doc == nil {
		return "", ""
	}
	var lines []string
	for _, c := range fn.Doc.List {
		t := strings.TrimPrefix(c.Text, "//")
		lines = append(lines, strings.TrimPrefix(t, " "))
	}
	// Drop the "name does X" convention from the first line: the path is
	// already the subject, and "containerTrade trades" reads as a stutter.
	if len(lines) > 0 {
		lines[0] = strings.TrimSpace(strings.TrimPrefix(lines[0], fn.Name.Name))
		if lines[0] != "" {
			lines[0] = strings.ToUpper(lines[0][:1]) + lines[0][1:]
		}
	}
	blank := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			blank = i
			break
		}
	}
	if blank == -1 {
		return strings.Join(lines, " "), ""
	}
	return strings.Join(lines[:blank], " "), strings.TrimSpace(strings.Join(lines[blank+1:], "\n"))
}

// fieldsOf reads a handler's input struct, following a named type and
// flattening an embedded one.
func fieldsOf(fn *ast.FuncDecl, types map[string]*ast.StructType) (body, query []field) {
	if fn.Type.Params != nil && len(fn.Type.Params.List) == 2 {
		body = structFields(fn.Type.Params.List[1].Type, types)
	}
	// A raw handler takes (w, r) and reads its own query string.
	src := &strings.Builder{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok {
			src.WriteString(lit.Value)
			src.WriteString(" ")
		}
		if sel, ok := n.(*ast.SelectorExpr); ok {
			src.WriteString(sel.Sel.Name)
			src.WriteString(" ")
		}
		return true
	})
	flat := src.String()
	seen := map[string]bool{}
	for _, m := range reQuery.FindAllStringSubmatch(rawOf(fn), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			query = append(query, field{name: m[1], kind: "string"})
		}
	}
	if strings.Contains(flat, "blockCoords") {
		for _, axis := range []string{"x", "y", "z"} {
			query = append(query, field{name: axis, kind: "integer",
				comment: "block coordinate; all three are required"})
		}
	}
	return body, query
}

// rawOf renders a function back to something the query regexp can read. The
// printer would be tidier, but this only needs the string literals and the
// selector names, and it avoids a second pass over the file.
func rawOf(fn *ast.FuncDecl) string {
	var b strings.Builder
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Get" && sel.Sel.Name != "Has" {
			return true
		}
		if len(call.Args) == 1 {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok {
				b.WriteString(`Query().Get(` + lit.Value + `)`)
			}
		}
		return true
	})
	return b.String()
}

func structFields(expr ast.Expr, types map[string]*ast.StructType) []field {
	var st *ast.StructType
	switch t := expr.(type) {
	case *ast.StructType:
		st = t
	case *ast.Ident:
		st = types[t.Name]
	}
	if st == nil {
		return nil
	}
	var out []field
	for _, f := range st.Fields.List {
		kind, items, optional := openAPIType(f.Type)
		jsonName := ""
		if f.Tag != nil {
			if m := reJSON.FindStringSubmatch(f.Tag.Value); m != nil {
				jsonName = m[1]
			}
		}
		if len(f.Names) == 0 {
			// Embedded: flatten it, which is what encoding/json does.
			if ident, ok := f.Type.(*ast.Ident); ok {
				out = append(out, structFields(ident, types)...)
			}
			continue
		}
		comment := ""
		if f.Doc != nil {
			var lines []string
			for _, c := range f.Doc.List {
				lines = append(lines, strings.TrimSpace(strings.TrimPrefix(c.Text, "//")))
			}
			comment = strings.Join(lines, " ")
		}
		for _, n := range f.Names {
			name := jsonName
			if name == "" {
				// No tag: encoding/json uses the field name, and matching is
				// case-insensitive, so either spelling works on the wire.
				name = n.Name
			}
			if name == "-" {
				continue
			}
			out = append(out, field{
				name: name, kind: kind, items: items,
				optional: optional, comment: comment,
			})
		}
	}
	return out
}

func openAPIType(expr ast.Expr) (kind, items string, optional bool) {
	switch t := expr.(type) {
	case *ast.StarExpr:
		k, i, _ := openAPIType(t.X)
		return k, i, true
	case *ast.Ident:
		switch t.Name {
		case "string":
			return "string", "", false
		case "bool":
			return "boolean", "", false
		case "int", "int8", "int32", "int64":
			return "integer", "", false
		case "float32", "float64":
			return "number", "", false
		case "blockRef":
			return "object", "", false
		}
		return "object", "", false
	case *ast.ArrayType:
		k, _, _ := openAPIType(t.Elt)
		return "array", k, false
	case *ast.MapType:
		return "object", "", false
	case *ast.SelectorExpr:
		return "string", "", false
	}
	return "", "", false
}

func render(routes []route) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("paths:\n")
	lastPath := ""
	for _, r := range routes {
		if r.path != lastPath {
			fmt.Fprintf(&b, "  %s:\n", r.path)
			lastPath = r.path
		}
		renderOperation(&b, r)
	}
	return b.String()
}

func renderOperation(b *strings.Builder, r route) {
	fmt.Fprintf(b, "    %s:\n", strings.ToLower(r.method))
	fmt.Fprintf(b, "      operationId: %s\n", r.handler)
	fmt.Fprintf(b, "      tags: [%s]\n", tagOf(r.path))
	if r.summary != "" {
		fmt.Fprintf(b, "      summary: %s\n", yamlString(r.summary))
	}
	if r.desc != "" {
		b.WriteString("      description: |\n")
		for _, line := range strings.Split(r.desc, "\n") {
			fmt.Fprintf(b, "        %s\n", line)
		}
	}
	renderQuery(b, r.query)
	renderBody(b, r.body)
	b.WriteString(responses)
}

func renderQuery(b *strings.Builder, query []field) {
	if len(query) == 0 {
		return
	}
	b.WriteString("      parameters:\n")
	for _, q := range query {
		fmt.Fprintf(b, "        - in: query\n          name: %s\n", q.name)
		fmt.Fprintf(b, "          schema: { type: %s }\n", q.kind)
		if q.comment != "" {
			fmt.Fprintf(b, "          description: %s\n", yamlString(q.comment))
		}
	}
}

func renderBody(b *strings.Builder, body []field) {
	if len(body) == 0 {
		return
	}
	b.WriteString("      requestBody:\n        required: false\n")
	b.WriteString("        content:\n          application/json:\n")
	b.WriteString("            schema:\n              type: object\n")
	b.WriteString("              properties:\n")
	for _, f := range body {
		fmt.Fprintf(b, "                %s:\n", f.name)
		fmt.Fprintf(b, "                  type: %s\n", f.kind)
		if f.kind == "array" && f.items != "" {
			fmt.Fprintf(b, "                  items: { type: %s }\n", f.items)
		}
		if f.comment != "" {
			fmt.Fprintf(b, "                  description: %s\n", yamlString(f.comment))
		}
	}
}

func tagOf(path string) string {
	switch {
	case strings.HasPrefix(path, "/container"):
		return "Containers"
	case path == "/trades" || path == "/recipes":
		return "Containers"
	}
	switch path {
	case "/state", "/inventory", "/block", "/ground", "/reach", "/lookingat", "/entities":
		return "Reading"
	case "/look", "/lookat", "/move", "/walk", "/fall", "/sneak":
		return "Movement"
	case "/dig", "/diglook", "/place", "/use":
		return "Blocks"
	case "/slot", "/hold", "/equip", "/drop", "/consume", "/craft":
		return "Items"
	case "/attack", "/swing", "/shoot", "/interact", "/interactat":
		return "Combat"
	}
	return "Workstations"
}

// yamlString quotes only when it has to, which keeps the document readable.
func yamlString(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, `:#{}[]&*!|>'"%@`) || strings.HasPrefix(s, " ") {
		return strconv.Quote(s)
	}
	return s
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genopenapi: "+format+"\n", args...)
	os.Exit(1)
}

const header = `# Generated by internal/gen/genopenapi from internal/control/routes.go.
# DO NOT EDIT — run: go run ./internal/gen/genopenapi > docs/openapi.yaml
#
# Request shapes come from each handler's own input struct, so what this says
# the server accepts is what it accepts. Response bodies are built as literals
# whose values are expressions, so only the envelope is described in full; the
# per-verb keys are named in each guide page, which also carries a captured
# example of every endpoint.
openapi: 3.1.0
info:
  title: understudy-client control API
  version: "1"
  summary: One headless Minecraft bot, over HTTP.
  description: |
    Start a bot with ` + "`-control <port>`" + ` and it serves this API, so a test suite in
    any language can drive it.

    There is **no authentication**. Bind it to loopback.

    Every endpoint reports what actually happened rather than what was asked
    for. The server accepts and silently ignores a great deal — a click on a
    stale window, a trade with a villager that has run out, a block placed
    inside an entity — so these verbs confirm through observed state instead.
  license:
    name: Apache-2.0
    identifier: Apache-2.0
servers:
  - url: http://127.0.0.1:8181
    description: A bot started with -control 8181
tags:
  - name: Reading
    description: Answered from the client's own model. These never wait on the server.
  - name: Movement
    description: Aiming, walking, sprinting and falling.
  - name: Blocks
    description: Breaking and placing.
  - name: Items
    description: Holding, wearing, eating and dropping.
  - name: Combat
    description: Attacking, shooting and interacting with entities.
  - name: Containers
    description: Windows, and moving things through them.
  - name: Workstations
    description: Furnaces, anvils, looms and the rest. Each takes the block to work at.
components:
  schemas:
    Envelope:
      type: object
      description: |
        Every POST that succeeds returns where the bot ended up, plus whatever
        that verb measured.
      properties:
        ok: { type: boolean }
        x: { type: number }
        y: { type: number }
        z: { type: number }
        yaw: { type: number }
        pitch: { type: number }
    Error:
      type: object
      properties:
        error:
          type: string
          description: What went wrong, and usually what to do about it.
`

const responses = `      responses:
        "200":
          description: Done. A POST also carries the envelope.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Envelope" }
        "400":
          description: |
            You asked wrongly — bad JSON, an unknown field, an unparseable
            coordinate.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Error" }
        "409":
          description: |
            You asked correctly and the world said no — out of reach, dead, not
            in play, nothing to trade. Carries whatever the verb managed first.
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Error" }
`
