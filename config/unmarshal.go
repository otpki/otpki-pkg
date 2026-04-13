package config

import (
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/samber/oops"
)

func unmarshalMap(data map[string]any, target any, hooks []UnmarshalHook) error {
	// supports both mapstructure tags and automatic field name matching for structs without tags.
	// build decoder config
	config := &mapstructure.DecoderConfig{
		Result:           target,
		WeaklyTypedInput: true,
		// match field names when the tag is not present
		MatchName: flexibleMatchName,
	}

	// add hooks if provided
	if len(hooks) > 0 {
		config.DecodeHook = ComposeHooks(hooks...)
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return oops.In(ServiceName).Wrapf(err, "creating mapstructure decoder")
	}

	if err := decoder.Decode(data); err != nil {
		return oops.In(ServiceName).Wrapf(err, "decoding configuration map")
	}

	return nil
}

func flexibleMatchName(mapKey, fieldName string) bool {
	// match the following names:
	// exact match (e.g., "Host" -> Host)
	// case-insensitive match (e.g., "host" -> Host, "HOST" -> Host)
	// snake_case to PascalCase (e.g., "max_retries" -> MaxRetries)
	// kebab-case to PascalCase (e.g., "max-retries" -> MaxRetries)

	// exact match
	if mapKey == fieldName {
		return true
	}

	// case-insensitive match
	if strings.EqualFold(mapKey, fieldName) {
		return true
	}

	// convert mapKey to PascalCase and compare
	pascalKey := toPascalCase(mapKey)
	if pascalKey == fieldName {
		return true
	}

	// case-insensitive pascal comparison
	if strings.EqualFold(pascalKey, fieldName) {
		return true
	}

	return false
}

func toPascalCase(s string) string {
	// coverts snake_case or kebab-case to PascalCase
	if s == "" {
		return s
	}

	// replace hyphens with underscores for uniform handling
	s = strings.ReplaceAll(s, "-", "_")

	// split by underscore
	parts := strings.Split(s, "_")

	// capitalize each part
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}

	return strings.Join(parts, "")
}
