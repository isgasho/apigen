package apigen

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/iancoleman/strcase"
	"github.com/morikuni/failure"
	"golang.org/x/tools/imports"
)

type request struct {
	path  *structType
	query *structType
	body  *structType
}

func (r *request) toStruct() *structType {
	var s structType
	if r.path != nil && len(r.path.fields) != 0 {
		s.fields = append(s.fields, r.path.fields...)
	}
	if r.query != nil && len(r.query.fields) != 0 {
		s.fields = append(s.fields, r.query.fields...)
	}
	if r.body != nil {
		s.fields = append(s.fields, r.body.fields...)
	}
	return &s
}

type method struct {
	name   string
	method string
	url    string
	req    *request
	res    _type
}

type generator struct {
	writer  io.Writer
	b       strings.Builder
	err     error
	errOnce sync.Once

	servicesMu sync.Mutex
	services   map[string][]*method
}

func newGenerator(w io.Writer) *generator {
	return &generator{writer: w, services: make(map[string][]*method)}
}

func (g *generator) addMethod(service string, method *method) {
	g.servicesMu.Lock()
	defer g.servicesMu.Unlock()
	g.services[service] = append(g.services[service], method)
}

func (g *generator) generate(pkg string) error {
	g.comment("Code generated by apigen; DO NOT EDIT.")
	g.comment("github.com/ktr0731/apigen")
	g.w("")
	g._package(pkg)
	g._import("github.com/ktr0731/apigen/client") // Standard and exp packages will be imported by imports.Process.

	type service struct {
		name    string
		methods []*method
	}
	var services []*service
	for name, methods := range g.services {
		sort.Slice(methods, func(i, j int) bool {
			return methods[i].name < methods[j].name
		})
		services = append(services, &service{name, methods})
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].name < services[j].name
	})

	for _, service := range services {
		name := service.name
		methods := service.methods

		g.typeInterface(name, methods)

		implName := strcase.ToLowerCamel(name)
		g.definedType(&definedType{
			name: implName,
			_type: &structType{
				fields: []*structField{
					{
						_type: &definedType{
							pkg:     "github.com/ktr0731/apigen/client",
							name:    "Client",
							pointer: true,
						},
					},
				},
			},
		})

		g.function(
			&function{
				name: fmt.Sprintf("New%s", public(name)),
				args: args{{
					name: "opts",
					_type: &definedType{
						pkg:  "github.com/ktr0731/apigen/client",
						name: "Option",
					},
					variadic: true,
				}},
				retVals: retVals{{_type: &definedType{name: public(name)}}},
			},
			func() {
				g.wf("return &%s{Client: client.New(opts...)}", strcase.ToLowerCamel(name))
			},
		)

		for _, m := range methods {
			g.method(implName, m)
		}

		for _, m := range methods {
			g.definedType(&definedType{
				name:  m.name + "Request",
				_type: m.req.toStruct(),
			})
			g.definedType(&definedType{
				name:  m.name + "Response",
				_type: m.res,
			})
		}
	}

	out, err := g.gen()
	if err != nil {
		return failure.Wrap(err)
	}

	fmt.Println(out)

	b, err := imports.Process("", []byte(out), &imports.Options{
		AllErrors: true,
		Comments:  true,
		TabIndent: true,
		TabWidth:  8,
	})
	if err != nil {
		return failure.Wrap(err)
	}

	if _, err := g.writer.Write(b); err != nil {
		return failure.Wrap(err)
	}

	return nil
}

func (g *generator) comment(content string) {
	g.wf("// %s", content)
}

func (g *generator) _package(name string) {
	g.wf("package %s", name)
}

func (g *generator) _import(paths ...string) {
	g.w("import (")
	for _, path := range paths {
		g.w(strconv.Quote(path))
	}
	g.w(")")
}

func (g *generator) typeInterface(name string, methods []*method) {
	g.wf("type %s interface{", name)
	for _, m := range methods {
		g.wf("%s(ctx context.Context, req *%s) (*%s, error)", m.name, m.name+"Request", m.name+"Response")
	}
	g.w("}")
	g.w("")
}

func (g *generator) method(recv string, m *method) {
	g.wf("func (c *%s) %s(ctx context.Context, req *%s) (*%s, error) {", recv, m.name, m.name+"Request", m.name+"Response")

	if m.req.query != nil {
		g.w("query := url.Values{")
		for _, f := range m.req.query.fields {
			if f._type == typeString {
				g.wf("%q: []string{req.%s},", f.meta["key"], f.name)
			} else {
				g.wf("%q: req.%s,", f.meta["key"], f.name)
			}
		}
		g.w("}.Encode()")
		g.w("")
	}

	var v string
	if m.req.path != nil {
		var params []string
		for _, f := range m.req.path.fields {
			params = append(params, fmt.Sprintf("req.%s", f.name))
		}
		v = fmt.Sprintf(`fmt.Sprintf(%q, %s)`, m.url, strings.Join(params, ", "))
	} else {
		v = strconv.Quote(m.url)
	}

	g.wf(`u, err := url.Parse(%s)`, v)
	g.w("if err != nil {")
	g.w("return nil, err")
	g.w("}")
	g.w("")

	if m.req.query != nil {
		g.w("u.RawQuery = query")
		g.w("")
	}

	g.wf("var res %s", m.name+"Response")
	g.wf("err = c.Do(ctx, %q, u, req.Body, &res)", m.method)
	g.w("return &res, err")
	g.w("}")
	g.w("")
}

func (g *generator) definedType(t *definedType) {
	if t.pkg != "" {
		return // Declared by another package.
	}

	g.wf("type %s %s\n", t.name, t._type.String())

	g.dependsTypes(t._type)
}

func (g *generator) dependsTypes(t _type) {
	switch v := t.(type) {
	case *definedType:
		g.definedType(v)
	case *structType:
		for _, f := range v.fields {
			g.dependsTypes(f._type)
		}
	case *sliceType:
		g.dependsTypes(v.elemType)
	}
}

func (g *generator) function(f *function, fn func()) {
	g.wf("func %s(%s) %s {", f.name, f.args, f.retVals)
	fn()
	g.w("}")
	g.w("")
}

func (g *generator) w(s string) {
	if g.err != nil {
		return
	}

	_, err := io.WriteString(&g.b, s+"\n")
	g.error(err)
}

func (g *generator) wf(f string, a ...interface{}) {
	if g.err != nil {
		return
	}

	_, err := fmt.Fprintf(&g.b, f+"\n", a...)
	g.error(err)
}

func (g *generator) error(err error) {
	if err == nil {
		return
	}
	g.errOnce.Do(func() {
		g.err = err
	})
}

func (g *generator) gen() (string, error) {
	if g.err != nil {
		return "", g.err
	}

	return g.b.String(), nil
}

type arg struct {
	name     string
	_type    _type
	variadic bool
}

type args []*arg

func (args args) String() string {
	as := make([]string, 0, len(args))
	for i, a := range args {
		s := a.name
		if i+1 == len(args) && a.variadic {
			s += fmt.Sprintf(" ...%s", a._type.String())
		} else {
			s += fmt.Sprintf(" %s", a._type.String())
		}
		as = append(as, s)
	}

	return strings.Join(as, ", ")
}

type retVal struct {
	_type _type
}

type retVals []*retVal

func (vals retVals) String() string {
	if len(vals) == 0 {
		return ""
	}

	vs := make([]string, 0, len(vals))
	for _, v := range vals {
		vs = append(vs, v._type.String())
	}

	if len(vals) > 1 {
		return fmt.Sprintf("(%s)", strings.Join(vs, ", "))
	}

	return strings.Join(vs, ", ")
}

type function struct {
	name    string
	args    args
	retVals retVals
}
