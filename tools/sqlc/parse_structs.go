package sqlc

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"unicode"
)

// Field represents a struct field with type information
type Field struct {
	Name     string
	Type     string
	JSONName string
}

func ParseStructs(filename string) (map[string][]Field, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	structs := make(map[string][]Field)

	// walk through AST to find struct definitions
	ast.Inspect(node, func(n ast.Node) bool {
		// assure the node is a generic declaration node
		// and that it specifies a type
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			return true
		}

		// for all the types in this node
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			// if the type is a struct
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// parse its fields
			var fields []Field
			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					fields = append(fields, Field{
						Name:     name.Name,
						Type:     resolveType(field.Type),
						JSONName: getJSONTag(field),
					})
				}
			}

			// struct name: fields
			structs[typeSpec.Name.Name] = fields
		}

		return true
	})

	return structs, nil
}

func resolveType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		// Basic types like string, int or local structs
		return t.Name
	case *ast.SelectorExpr:
		// Types from other packages, e.g., time.Time
		return fmt.Sprintf("%s.%s", resolveType(t.X), t.Sel.Name)
	case *ast.StarExpr:
		// Pointer types
		return "*" + resolveType(t.X)
	case *ast.ArrayType:
		// Slices and Arrays
		return "[]" + resolveType(t.Elt)
	case *ast.MapType:
		// Map types
		return fmt.Sprintf("map[%s]%s", resolveType(t.Key), resolveType(t.Value))
	case *ast.InterfaceType:
		return "any"
	default:
		// Fallback or more complex types (chan, func, etc.)
		return "unknown"
	}
}

func getJSONTag(field *ast.Field) string {
	// try to get the tag if it exists
	if field.Tag != nil {
		tagValue := strings.Trim(field.Tag.Value, "`")
		tag := reflect.StructTag(tagValue)
		jsonValue := tag.Get("json")

		if jsonValue != "" {
			parts := strings.Split(jsonValue, ",")
			name := parts[0]
			// if the name is "-", it's technically "ignored",
			if name != "-" {
				return name
			}
		}
	}

	// try to fall back to the field name as snake case
	if len(field.Names) > 0 {
		return ToSnakeCase(field.Names[0].Name)
	}

	// fallback for embedded types to the type name as snake case
	return ToSnakeCase(resolveType(field.Type))
}

func ToSnakeCase(s string) string {
	var res strings.Builder
	runes := []rune(s)

	for i := range runes {
		r := runes[i]

		// if it's uppercase, we need to decide if we add an underscore
		if unicode.IsUpper(r) {
			// add underscore if:
			// not at the start of string
			// previous char is lowercase (e.g., 'aB' -> 'a_b')
			// next char is lowercase (e.g., 'IDProp' -> 'id_prop' - handles acronyms)
			if i > 0 {
				prevLower := unicode.IsLower(runes[i-1])
				nextExists := i+1 < len(runes)
				nextLower := nextExists && unicode.IsLower(runes[i+1])

				if prevLower || nextLower {
					res.WriteRune('_')
				}
			}
			res.WriteRune(unicode.ToLower(r))
		} else {
			res.WriteRune(r)
		}
	}
	return res.String()
}
