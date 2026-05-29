package authcommon

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// AuthRule represents a parsed @auth annotation
type AuthRule struct {
	Resource string
	Action   string
	ObjectID string
}

// MethodInfo contains information about a method with auth annotations
type MethodInfo struct {
	Name       string
	AuthRules  []AuthRule
	Params     []*ast.Field
	Results    []*ast.Field
	HasContext bool // first param ctx context.Context
}

// InterfaceInfo contains information about an interface with auth annotations
type InterfaceInfo struct {
	Name    string
	Methods []MethodInfo
}

// ServiceInfo contains information about a service/package with auth annotations
type ServiceInfo struct {
	PackagePath string
	PackageName string
	Interfaces  []InterfaceInfo
}

// PackageWithAuth extends ServiceInfo with the actual package object
type PackageWithAuth struct {
	Path       string
	Package    *packages.Package
	Interfaces []InterfaceInfo
}

// authLine regex for parsing @auth annotations
// Example: // @auth:resource=RESOURCE_CERTIFICATE_AUTHORITY,action=ACTION_READ,object_id=caID
var authLine = regexp.MustCompile(`@auth\s*:\s*resource=([^,]+)\s*,\s*action=([^,]+)(?:\s*,\s*object_id=([^\s,]+))?\s*$`)

// ParseAuthRules parses @auth annotations from a comment group
func ParseAuthRules(cg *ast.CommentGroup) []AuthRule {
	if cg == nil {
		return nil
	}
	var rules []AuthRule
	for _, c := range cg.List {
		txt := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if !strings.Contains(txt, "@auth") {
			continue
		}
		m := authLine.FindStringSubmatch(txt)
		if m == nil {
			continue
		}
		rules = append(rules, AuthRule{
			Resource: strings.TrimSpace(m[1]),
			Action:   strings.TrimSpace(m[2]),
			ObjectID: strings.TrimSpace(m[3]),
		})
	}
	return rules
}

// DiscoverPackagesWithAuth discovers all packages with @auth annotations in the given root path
func DiscoverPackagesWithAuth(rootPath string, verbose bool) ([]PackageWithAuth, error) {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedSyntax | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedTypesInfo,
		Tests: false,
	}

	// ensure root path is a valid go package pattern
	if !strings.HasPrefix(rootPath, "./") {
		rootPath = "./" + rootPath
	}
	if !strings.HasSuffix(rootPath, "/...") {
		rootPath += "/..."
	}

	// load packages
	pkgs, err := packages.Load(cfg, rootPath)
	if err != nil {
		return nil, err
	}

	// filter out packages with compilation errors but continue processing others
	var validPkgs []*packages.Package
	for _, pkg := range pkgs {
		if len(pkg.Errors) == 0 {
			validPkgs = append(validPkgs, pkg)
		}
	}

	var pkgsWithAuth []PackageWithAuth

	for _, pkg := range validPkgs {
		// skip test packages
		if strings.HasSuffix(pkg.PkgPath, "_test") {
			continue
		}
		// check if any file in this package has @auth annotations and extract interfaces
		ifaces := FindInterfaces(pkg.Syntax)
		if len(ifaces) > 0 {
			pkgsWithAuth = append(pkgsWithAuth, PackageWithAuth{
				Path:       pkg.PkgPath,
				Package:    pkg,
				Interfaces: ifaces,
			})
		}
	}

	return pkgsWithAuth, nil
}

// DiscoverServices discovers services with @auth annotations (for authmapgen compatibility)
func DiscoverServices(rootPath string, verbose bool) ([]ServiceInfo, error) {
	pkgsWithAuth, err := DiscoverPackagesWithAuth(rootPath, verbose)
	if err != nil {
		return nil, err
	}

	var services []ServiceInfo
	for _, pkgWithAuth := range pkgsWithAuth {
		services = append(services, ServiceInfo{
			PackagePath: pkgWithAuth.Path,
			PackageName: pkgWithAuth.Package.Name,
			Interfaces:  pkgWithAuth.Interfaces,
		})
	}

	return services, nil
}

// FindInterfaces finds all interfaces with @auth annotations in the given files
func FindInterfaces(files []*ast.File) []InterfaceInfo {
	var out []InterfaceInfo

	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				// check if this is a type declaration
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				// check if this is an interface
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}

				iface := InterfaceInfo{Name: ts.Name.Name}

				// extract methods
				for _, field := range it.Methods.List {
					// embedded interface? skip
					if len(field.Names) == 0 {
						continue
					}
					// make sure this is a function
					fn, ok := field.Type.(*ast.FuncType)
					if !ok {
						continue
					}
					m := MethodInfo{
						Name:      field.Names[0].Name,
						Params:    fieldsToSlice(fn.Params),
						Results:   fieldsToSlice(fn.Results),
						AuthRules: ParseAuthRules(field.Doc),
					}
					m.HasContext = hasLeadingContextParam(m.Params)

					iface.Methods = append(iface.Methods, m)
				}

				// check if any method has @auth annotation matches
				hasAny := false
				for _, m := range iface.Methods {
					if len(m.AuthRules) > 0 {
						hasAny = true
						break
					}
				}
				if !hasAny {
					continue
				}

				out = append(out, iface)
			}
		}
	}

	// stable ordering
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// fieldsToSlice converts a FieldList to a slice of Field pointers
func fieldsToSlice(fl *ast.FieldList) []*ast.Field {
	if fl == nil {
		return nil
	}
	return fl.List
}

// hasLeadingContextParam checks if the first parameter is context.Context
func hasLeadingContextParam(params []*ast.Field) bool {
	if len(params) == 0 {
		return false
	}
	p0 := params[0]
	if len(p0.Names) == 0 {
		return false
	}
	// type should look like context.Context
	sel, ok := p0.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkgIdent.Name == "context" && sel.Sel.Name == "Context"
}

// IsBuiltinType checks if a type name is a built-in Go type
func IsBuiltinType(name string) bool {
	builtinTypes := map[string]bool{
		"string": true, "bool": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
		"float32": true, "float64": true, "complex64": true, "complex128": true,
		"error": true, "byte": true, "rune": true,
	}
	return builtinTypes[name]
}

// IsInterfaceIdent checks if an identifier is an interface type using package type info
func IsInterfaceIdent(pkg *packages.Package, ident *ast.Ident) bool {
	// use the package's type info to check if this identifier is an interface
	if pkg.TypesInfo == nil || pkg.TypesInfo.Types == nil {
		return false
	}

	// get the type and object for this identifier
	typeAndValue := pkg.TypesInfo.Types[ident]
	if typeAndValue.Type == nil {
		return false
	}

	// check if the underlying type is an interface
	if namedType, ok := typeAndValue.Type.(*types.Named); ok {
		if _, isInterface := namedType.Underlying().(*types.Interface); isInterface {
			return true
		}
	}

	// also check if it's directly an interface type
	if _, isInterface := typeAndValue.Type.Underlying().(*types.Interface); isInterface {
		return true
	}

	return false
}

// ExprString converts an AST expression to a string representation
func ExprString(e ast.Expr) string {
	var buf bytes.Buffer
	_ = format.Node(&buf, token.NewFileSet(), e)
	return buf.String()
}

// IsErrorType checks if an expression represents the error type
func IsErrorType(e ast.Expr) bool {
	if id, ok := e.(*ast.Ident); ok && id.Name == "error" {
		return true
	}
	return false
}

// LastIsError checks if the last return value is an error
func LastIsError(results []*ast.Field) bool {
	if len(results) == 0 {
		return false
	}
	last := results[len(results)-1]
	return IsErrorType(last.Type)
}

// CallArgList extracts parameter names from a field list
func CallArgList(params []*ast.Field) string {
	var names []string
	for _, p := range params {
		for _, n := range p.Names {
			names = append(names, n.Name)
		}
	}
	return strings.Join(names, ", ")
}

// WriteFieldList writes a field list to a buffer in Go syntax
func WriteFieldList(b *bytes.Buffer, fields []*ast.Field) {
	first := true
	for _, f := range fields {
		typ := ExprString(f.Type)
		if len(f.Names) == 0 {
			if !first {
				fmt.Fprintf(b, ", ")
			}
			fmt.Fprintf(b, "%s", typ)
			first = false
			continue
		}
		for _, n := range f.Names {
			if !first {
				fmt.Fprintf(b, ", ")
			}
			fmt.Fprintf(b, "%s %s", n.Name, typ)
			first = false
		}
	}
}

// WriteResults writes return values to a buffer in Go syntax
func WriteResults(b *bytes.Buffer, results []*ast.Field) {
	if len(results) == 0 {
		return
	}
	if len(results) == 1 && len(results[0].Names) == 0 {
		fmt.Fprintf(b, " %s", ExprString(results[0].Type))
		return
	}
	fmt.Fprintf(b, " (")
	first := true
	for _, r := range results {
		typ := ExprString(r.Type)
		if len(r.Names) == 0 {
			if !first {
				fmt.Fprintf(b, ", ")
			}
			fmt.Fprintf(b, "%s", typ)
			first = false
			continue
		}
		for _, n := range r.Names {
			if !first {
				fmt.Fprintf(b, ", ")
			}
			fmt.Fprintf(b, "%s %s", n.Name, typ)
			first = false
		}
	}
	fmt.Fprintf(b, ")")
}

// ZeroExprs generates zero value expressions for a list of fields
func ZeroExprs(pkg *packages.Package, fields []*ast.Field) []string {
	var out []string
	for _, f := range fields {
		typ := f.Type
		count := len(f.Names)
		if count == 0 {
			count = 1
		}
		for i := 0; i < count; i++ {
			out = append(out, ZeroExprForType(pkg, typ))
		}
	}
	return out
}

// ZeroExprForType generates a zero value expression for a given type
func ZeroExprForType(pkg *packages.Package, t ast.Expr) string {
	switch tt := t.(type) {
	case *ast.Ident:
		switch tt.Name {
		case "string":
			return `""`
		case "bool":
			return "false"
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
			"float32", "float64", "complex64", "complex128":
			return "0"
		default:
			if IsInterfaceIdent(pkg, tt) {
				return "nil"
			}
			return fmt.Sprintf("%s{}", tt.Name)
		}
	case *ast.StarExpr, *ast.SliceExpr, *ast.MapType, *ast.FuncType, *ast.InterfaceType, *ast.ChanType:
		return "nil"
	case *ast.ArrayType:
		return fmt.Sprintf("%s{}", ExprString(t))
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s{}", ExprString(t))
	default:
		return fmt.Sprintf("%s{}", ExprString(t))
	}
}

// CollectImportsFromFile extracts import statements from an AST file
func CollectImportsFromFile(file *ast.File, imports map[string]string) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}

		for _, spec := range genDecl.Specs {
			importSpec, ok := spec.(*ast.ImportSpec)
			if !ok {
				continue
			}

			path := strings.Trim(importSpec.Path.Value, `"`)
			if path == "" {
				continue
			}

			if importSpec.Name != nil {
				if _, exists := imports[importSpec.Name.Name]; !exists {
					imports[importSpec.Name.Name] = path
				}
			} else {
				parts := strings.Split(path, "/")
				alias := parts[len(parts)-1]
				if _, exists := imports[alias]; !exists {
					imports[alias] = path
				}
			}
		}
	}
}

// ExtractUsedTypes recursively extracts types used in expressions
func ExtractUsedTypes(expr ast.Expr, allImports, usedImports map[string]string) {
	switch e := expr.(type) {
	case *ast.Ident:
		if IsBuiltinType(e.Name) {
			return
		}
	case *ast.SelectorExpr:
		if pkgIdent, ok := e.X.(*ast.Ident); ok {
			pkgAlias := pkgIdent.Name
			if importPath, exists := allImports[pkgAlias]; exists {
				usedImports[pkgAlias] = importPath
			}
		}
	case *ast.StarExpr:
		ExtractUsedTypes(e.X, allImports, usedImports)
	case *ast.SliceExpr:
		ExtractUsedTypes(e.X, allImports, usedImports)
	case *ast.ArrayType:
		ExtractUsedTypes(e.Elt, allImports, usedImports)
	case *ast.MapType:
		ExtractUsedTypes(e.Key, allImports, usedImports)
		ExtractUsedTypes(e.Value, allImports, usedImports)
	}
}

// WriteGeneratedFile writes generated code to a file with proper error handling
func WriteGeneratedFile(outputPath, toolName string, code []byte, verbose bool) error {
	// ensure directory exists
	dir := filepath.Dir(outputPath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	err = os.WriteFile(outputPath, code, 0644)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", outputPath, err)
	}

	if verbose {
		log.Printf("Generated %s at %s\n", toolName, outputPath)
	}

	return nil
}
