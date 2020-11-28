package apigen

import (
	"fmt"
	"go/format"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/iancoleman/strcase"
	"github.com/morikuni/failure"
)

type Generator struct {
	writer   io.Writer
	b        strings.Builder
	err      error
	errOnce  sync.Once
	services map[string][]*Method
}

func NewGenerator(w io.Writer) *Generator {
	return &Generator{writer: w, services: make(map[string][]*Method)}
}

func (g *Generator) Add(service string, method *Method) {
	g.services[service] = append(g.services[service], method)
}

func (g *Generator) Generate() error {
	g.comment("Code generated by apigen; DO NOT EDIT.")
	g.comment("github.com/ktr0731/apigen")
	g.w("")
	g._package("main")
	g._import("context")

	for name, methods := range g.services {
		g.typeInterface(name, methods)

		g.definedType(&definedType{
			name: strcase.ToLowerCamel(name),
			_type: &structType{
				fields: []*structField{
					{name: "httpClient", _type: &definedType{pkg: "net/http", name: "Client"}},
				},
			},
		})

		for _, m := range methods {
			g.definedType(&definedType{
				name:  m.Name + "Request",
				_type: m.req,
			})
			g.definedType(&definedType{
				name:  m.Name + "Response",
				_type: m.res,
			})
		}
	}

	out, err := g.gen()
	if err != nil {
		return failure.Wrap(err)
	}

	fmt.Println(out)

	b, err := format.Source([]byte(out))
	if err != nil {
		return failure.Wrap(err)
	}

	if _, err := g.writer.Write(b); err != nil {
		return failure.Wrap(err)
	}

	return nil
}

func (g *Generator) comment(content string) {
	g.wf("// %s", content)
}

func (g *Generator) _package(name string) {
	g.wf("package %s", name)
}

func (g *Generator) _import(paths ...string) {
	g.w("import (")
	for _, path := range paths {
		g.w(strconv.Quote(path))
	}
	g.w(")")
}

func (g *Generator) typeInterface(name string, methods []*Method) {
	g.wf("type %s interface{", name)
	for _, m := range methods {
		g.wf("%s(ctx context.Context, req *%s) (*%s, error)", m.Name, m.Name+"Request", m.Name+"Response")
	}
	g.w("}")
	g.w("")
}

func (g *Generator) definedType(t *definedType) {
	if t.pkg != "" {
		return // Declared by another package.
	}

	g.wf("type %s %s\n", t.name, t._type.String())

	g.dependsTypes(t._type)
}

func (g *Generator) dependsTypes(t _type) {
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

func (g *Generator) w(s string) {
	if g.err != nil {
		return
	}

	_, err := io.WriteString(&g.b, s+"\n")
	g.error(err)
}

func (g *Generator) wf(f string, a ...interface{}) {
	if g.err != nil {
		return
	}

	_, err := fmt.Fprintf(&g.b, f+"\n", a...)
	g.error(err)
}

func (g *Generator) error(err error) {
	if err == nil {
		return
	}
	g.errOnce.Do(func() {
		g.err = err
	})
}

func (g *Generator) gen() (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return g.b.String(), nil
}
