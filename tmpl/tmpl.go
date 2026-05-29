package tmpl

import (
	"bytes"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/samber/oops"
)

type Template struct {
	raw  string
	tmpl *template.Template
}

// Create template
func New(raw string) (*Template, error) {
	tmpl, err := template.New("").Option("missingkey=error").Parse(raw)
	if err != nil {
		return nil, oops.Errorf("invalid template: %w", err)
	}
	return &Template{raw: raw, tmpl: tmpl}, nil
}

// Template string
func (t *Template) String() string {
	return t.raw
}

// Render a resolved template str with data
func (t *Template) Render(data any) (string, error) {
	var buf bytes.Buffer
	if err := t.tmpl.Execute(&buf, data); err != nil {
		return "", oops.Errorf("render failed: %w", err)
	}
	return buf.String(), nil
}

// Render multiple resolved template strings with data
func RenderAll(data any, raws []string) ([]string, error) {
	out := make([]string, len(raws))
	for i, raw := range raws {
		t, err := New(raw)
		if err != nil {
			return nil, err
		}
		out[i], err = t.Render(data)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Check for keys not in valid data set, err includes invalid keys
func (t *Template) ValidateKeys(allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, k := range allowed {
		set[k] = struct{}{}
	}

	refs := fields(t.tmpl.Root)
	var unknown []string
	for _, r := range refs {
		if _, ok := set[r]; !ok {
			unknown = append(unknown, r)
		}
	}
	if len(unknown) > 0 {
		return oops.Errorf("unknown template keys: %s; allowed: %s",
			strings.Join(unknown, ", "), strings.Join(allowed, ", "))
	}
	return nil
}

// Walk template, return all referenced field names
func fields(node parse.Node) []string {
	var out []string
	seen := make(map[string]bool)

	var walk func(parse.Node)
	walk = func(n parse.Node) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *parse.ListNode:
			for _, child := range v.Nodes {
				walk(child)
			}
		case *parse.ActionNode:
			walk(v.Pipe)
		case *parse.PipeNode:
			for _, cmd := range v.Cmds {
				walk(cmd)
			}
		case *parse.CommandNode:
			for _, arg := range v.Args {
				walk(arg)
			}
		case *parse.FieldNode:
			if len(v.Ident) > 0 && !seen[v.Ident[0]] {
				seen[v.Ident[0]] = true
				out = append(out, v.Ident[0])
			}
		case *parse.IfNode:
			walk(v.Pipe)
			walk(v.List)
			walk(v.ElseList)
		case *parse.RangeNode:
			walk(v.Pipe)
			walk(v.List)
			walk(v.ElseList)
		case *parse.WithNode:
			walk(v.Pipe)
			walk(v.List)
			walk(v.ElseList)
		}
	}
	walk(node)
	return out
}
