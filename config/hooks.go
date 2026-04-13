package config

import (
	"reflect"
	"strings"
	"time"

	"golang.org/x/exp/constraints"

	"github.com/go-viper/mapstructure/v2"
	"github.com/samber/oops"
)

type UnmarshalHook = mapstructure.DecodeHookFunc

// StringToTimeDurationHook converts strings to time.Duration.
func StringToTimeDurationHook() UnmarshalHook {
	return mapstructure.StringToTimeDurationHookFunc()
}

// StringToSliceHook converts comma-separated strings to slices.
func StringToSliceHook(sep string) UnmarshalHook {
	return mapstructure.StringToSliceHookFunc(sep)
}

// StringToTimeHook converts strings to time.Time with a given layout.
func StringToTimeHook(layout string) UnmarshalHook {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		if t != reflect.TypeFor[time.Time]() {
			return data, nil
		}

		return time.Parse(layout, data.(string))
	}
}

// EnumHook creates a hook for parsing enum values from strings.
func EnumHook[T constraints.Integer](enumMap map[string]T, targetType reflect.Type) UnmarshalHook {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if t != targetType {
			return data, nil
		}

		// if it's already the right type, pass through
		if f == targetType {
			return data, nil
		}

		// if it's a string, look it up in the map
		if f.Kind() == reflect.String {
			str := data.(string)
			if val, ok := enumMap[str]; ok {
				return val, nil
			}
			return nil, oops.In(ServiceName).Errorf("invalid enum value: %s", str)
		}

		// if it's a numeric type, validate it in the map
		if f.Kind() >= reflect.Int && f.Kind() <= reflect.Int64 {
			val := reflect.ValueOf(data).Convert(targetType)
			// check if this numeric value exists in our enum map
			found := false
			for _, v := range enumMap {
				if reflect.ValueOf(v).Int() == val.Int() {
					found = true
					break
				}
			}
			if found {
				return val.Interface(), nil
			}
			return nil, oops.In(ServiceName).Errorf("invalid enum value: %v", data)
		}

		return data, nil
	}
}

// EnumArrayHook creates a hook for parsing arrays of enum values from comma-separated strings.
// This hook should be used in combination with EnumHook. The EnumArrayHook splits the
// comma-separated string into []string, and then EnumHook converts each element to the enum type.
func EnumArrayHook(targetArrayType reflect.Type) UnmarshalHook {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if t != targetArrayType {
			return data, nil
		}

		// if we weren't provided with a string, pass through
		if f.Kind() != reflect.String {
			return data, nil
		}

		// if it is a string, convert it to a string array
		// the individual enum hook will handle converting each string to the enum type
		str := data.(string)
		if str == "" {
			return []string{}, nil
		}

		return splitAndTrim(str, ","), nil
	}
}

// BitFlagHook creates a hook for parsing bit flags from comma-separated strings.
func BitFlagHook[T constraints.Signed](flagMap map[string]T, targetType reflect.Type) UnmarshalHook {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if t != targetType {
			return data, nil
		}

		// if it's already the right type, pass through
		if f == targetType {
			return data, nil
		}

		// if it's a string, parse comma-separated flags
		if f.Kind() == reflect.String {
			str := data.(string)
			if str == "" {
				var zero T
				return zero, nil
			}

			var result T
			for _, part := range splitAndTrim(str, ",") {
				if val, ok := flagMap[part]; ok {
					result |= val
				} else {
					return nil, oops.In(ServiceName).Errorf("invalid flag value: %s", part)
				}
			}
			return result, nil
		}

		// if it's a numeric type, validate it
		if f.Kind() >= reflect.Int && f.Kind() <= reflect.Int64 {
			val := reflect.ValueOf(data).Convert(targetType)
			return val.Interface(), nil
		}

		return data, nil
	}
}

func splitAndTrim(s, sep string) []string {
	// split a string by a separate and trim the whitespace of the remaining strings
	if s == "" {
		return []string{}
	}

	var parts []string
	for part := range strings.SplitSeq(s, sep) {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

// ComposeHooks composes multiple unmarshal hooks into one.
func ComposeHooks(hooks ...UnmarshalHook) UnmarshalHook {
	return mapstructure.ComposeDecodeHookFunc(hooks...)
}
