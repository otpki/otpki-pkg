package sqlc

import (
	"fmt"
	"strings"
)

// FieldMapping represents how a model field maps to a params field
type FieldMapping struct {
	ModelField  string
	ParamsField string
	ModelType   string
	ParamsType  string
	Conversion  string
	IsPgxType   bool
	JSONName    string // DB JSON name (snake_case)
}

// ConversionResult represents the result of conversion generation
type ConversionResult struct {
	ModelStructName    string
	ParamsStructName   string
	ModelStructFields  []Field
	ParamsStructFields []Field
	FieldMappings      []FieldMapping
	NeedsConversion    bool
}

// UpdateConversionResult represents the result of update conversion generation
type UpdateConversionResult struct {
	ModelStructName    string
	UpdateStructName   string
	ModelStructFields  []Field
	UpdateStructFields []Field
	FieldMappings      []FieldMapping
	HasFieldMask       bool
	NeedsConversion    bool
	HasID              bool
}

// FindMatchingField finds the best matching field between model and params
func FindMatchingField(modelField Field, paramsFields []Field) *Field {
	// Try exact match first
	for _, paramsField := range paramsFields {
		if paramsField.Name == modelField.Name {
			return &paramsField
		}
	}

	// Try case-insensitive match
	modelLower := strings.ToLower(modelField.Name)
	for _, paramsField := range paramsFields {
		if strings.ToLower(paramsField.Name) == modelLower {
			return &paramsField
		}
	}

	return nil
}

// IsPgxTypeField checks if a field type is a PGX type
func IsPgxTypeField(fieldType string) bool {
	pgxTypes := []string{
		"pgtype.Text", "pgtype.Int4", "pgtype.Int8", "pgtype.Bool",
		"pgtype.Timestamptz", "pgtype.Timestamp", "pgtype.Numeric",
	}
	for _, pgxType := range pgxTypes {
		if strings.Contains(fieldType, pgxType) {
			return true
		}
	}
	return false
}

func IsUUIDNullType(fieldType string) bool {
	return strings.Contains(fieldType, "uuid.NullUUID")
}

// GeneratePgxConversion generates conversion code for PGX types
func GeneratePgxConversion(modelFieldName, pgxType string) string {
	switch {
	case strings.Contains(pgxType, "pgtype.Text"):
		return fmt.Sprintf("pgtype.Text{String: model.%s, Valid: true}", modelFieldName)
	case strings.Contains(pgxType, "pgtype.Int4"):
		return fmt.Sprintf("pgtype.Int4{Int32: int32(model.%s), Valid: true}", modelFieldName)
	case strings.Contains(pgxType, "pgtype.Int8"):
		return fmt.Sprintf("pgtype.Int8{Int64: model.%s, Valid: true}", modelFieldName)
	case strings.Contains(pgxType, "pgtype.Bool"):
		return fmt.Sprintf("pgtype.Bool{Bool: model.%s, Valid: true}", modelFieldName)
	case strings.Contains(pgxType, "pgtype.Timestamptz"), strings.Contains(pgxType, "pgtype.Timestamp"):
		return fmt.Sprintf("pgtype.Timestamptz{Time: model.%s, Valid: true}", modelFieldName)
	default:
		return fmt.Sprintf("model.%s", modelFieldName)
	}
}

// CheckIfFieldNeedsConversion determines if a field actually needs conversion logic
func CheckIfFieldNeedsConversion(modelField, paramsField Field, conversion string) bool {
	// Only mark as needing conversion if there's actual logic beyond simple assignment
	return modelField.Type != paramsField.Type &&
		!strings.HasPrefix(conversion, "model.") &&
		!strings.HasPrefix(conversion, "&model.") &&
		!strings.Contains(conversion, "int32(model.") &&
		!strings.Contains(conversion, "int64(model.") &&
		(strings.Contains(conversion, "pgtype.") ||
			strings.Contains(conversion, "func()") ||
			strings.Contains(conversion, "if ") ||
			strings.Contains(conversion, "else "))
}

// CheckIfStructHasField checks if a struct has a specific field
func CheckIfStructHasField(fields []Field, fieldName string) bool {
	for _, field := range fields {
		if field.Name == fieldName {
			return true
		}
	}
	return false
}

// CheckIfBothStructsHaveID checks if both model and params structs have compatible ID fields
func CheckIfBothStructsHaveID(modelFields, paramsFields []Field) bool {
	hasModelID := CheckIfStructHasField(modelFields, "ID")
	hasParamsID := CheckIfStructHasField(paramsFields, "ID")
	return hasModelID && hasParamsID
}

// GenerateFieldMappings creates field mappings between model and params fields
func GenerateFieldMappings(modelFields []Field, paramsFields []Field, skipID bool) []FieldMapping {
	var fieldMappings []FieldMapping

	for _, modelField := range modelFields {
		// Skip ID if requested
		if skipID && modelField.Name == "ID" {
			continue
		}

		// Find matching params field
		paramsField := FindMatchingField(modelField, paramsFields)
		if paramsField != nil {
			fieldMappings = append(fieldMappings, FieldMapping{
				ModelField:  modelField.Name,
				ParamsField: paramsField.Name,
				ModelType:   modelField.Type,
				ParamsType:  paramsField.Type,
				Conversion:  GenerateFieldConversion(modelField, *paramsField),
				IsPgxType:   IsPgxTypeField(paramsField.Type),
				JSONName:    modelField.JSONName,
			})
		}
	}

	return fieldMappings
}

// CheckIfNeedsPgxImport determines if pgx import is needed for the conversions
func CheckIfNeedsPgxImport(results []any) bool {
	for _, result := range results {
		switch r := result.(type) {
		case ConversionResult:
			for _, mapping := range r.FieldMappings {
				if mapping.IsPgxType && mapping.ModelType != mapping.ParamsType {
					return true
				}
			}
		case UpdateConversionResult:
			for _, mapping := range r.FieldMappings {
				if mapping.IsPgxType && mapping.ModelType != mapping.ParamsType {
					return true
				}
			}
		}
	}
	return false
}

// CheckIfNeedsUUIDImport determines if uuid import is needed for the conversions
func CheckIfNeedsUUIDImport(results []any) bool {
	for _, result := range results {
		switch r := result.(type) {
		case ConversionResult:
			for _, mapping := range r.FieldMappings {
				if strings.Contains(mapping.Conversion, "uuid.") {
					return true
				}
			}
		case UpdateConversionResult:
			for _, mapping := range r.FieldMappings {
				if strings.Contains(mapping.Conversion, "uuid.") {
					return true
				}
			}
		}
	}
	return false
}

// GenerateImportSection generates the import section for generated code
func GenerateImportSection(buf *strings.Builder, needsPgxType bool, needsUUID bool, includeProtobuf bool) {
	buf.WriteString("import (\n")
	if needsPgxType {
		buf.WriteString("\t\"github.com/jackc/pgx/v5/pgtype\"\n")
	}
	if needsUUID {
		buf.WriteString("\t\"github.com/google/uuid\"\n")
	}
	if includeProtobuf {
		buf.WriteString("\t\"google.golang.org/protobuf/proto\"\n")
		buf.WriteString("\t\"google.golang.org/protobuf/types/known/fieldmaskpb\"\n")
		buf.WriteString("\t\"strings\"\n")
	}
	buf.WriteString(")\n\n")
}
func GenerateFieldConversion(modelField, paramsField Field) string {
	// Handle basic pointer to non-pointer conversion
	if strings.HasPrefix(modelField.Type, "*") && !strings.HasPrefix(paramsField.Type, "*") {
		// Model field is pointer, params field is not - dereference with nil check
		nonPointerType := strings.TrimPrefix(modelField.Type, "*")
		if nonPointerType == paramsField.Type {
			return fmt.Sprintf("func() %s { if model.%s != nil { return *model.%s } else { return %s{} } }()",
				paramsField.Type, modelField.Name, modelField.Name, getZeroValue(paramsField.Type))
		}
	}

	// Handle basic non-pointer to pointer conversion
	if !strings.HasPrefix(modelField.Type, "*") && strings.HasPrefix(paramsField.Type, "*") {
		// Model field is not pointer, params field is pointer - create pointer
		nonPointerType := strings.TrimPrefix(paramsField.Type, "*")
		if nonPointerType == modelField.Type {
			return fmt.Sprintf("&model.%s", modelField.Name)
		}
	}

	// If we need to convert to a pgx type, do so
	if modelField.Type != paramsField.Type && IsPgxTypeField(paramsField.Type) {
		return GeneratePgxConversion(modelField.Name, paramsField.Type)
	}

	// Convert uuid.UUID to uuid.NullUUID for nullable sqlc UUID columns.
	if modelField.Type == "uuid.UUID" && IsUUIDNullType(paramsField.Type) {
		return fmt.Sprintf("func() uuid.NullUUID { if model.%s == uuid.Nil { return uuid.NullUUID{} }; return uuid.NullUUID{UUID: model.%s, Valid: true} }()", modelField.Name, modelField.Name)
	}

	// Handle common type conversions
	conversions := map[string]map[string]string{
		"int64": {
			"int32": fmt.Sprintf("int32(model.%s)", modelField.Name),
		},
		"int32": {
			"int64": fmt.Sprintf("int64(model.%s)", modelField.Name),
		},
	}

	if typeConversions, exists := conversions[modelField.Type]; exists {
		if conversion, exists := typeConversions[paramsField.Type]; exists {
			return conversion
		}
	}

	if typeConversions, exists := conversions[paramsField.Type]; exists {
		if conversion, exists := typeConversions[modelField.Type]; exists {
			return conversion
		}
	}

	// default to direct assignment
	return fmt.Sprintf("model.%s", modelField.Name)
}

func getZeroValue(typeStr string) string {
	switch typeStr {
	case "int", "int32", "int64":
		return "0"
	case "string":
		return `""`
	case "bool":
		return "false"
	case "time.Time":
		return "time.Time{}"
	default:
		// for custom types, try to use zero value syntax
		if strings.Contains(typeStr, ".") {
			parts := strings.Split(typeStr, ".")
			return fmt.Sprintf("%s{}", parts[len(parts)-1])
		}
		return fmt.Sprintf("%s{}", typeStr)
	}
}
