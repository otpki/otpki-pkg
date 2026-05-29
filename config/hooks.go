package config

import (
	"encoding"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/samber/oops"
)

// textUnmarshalerHook lets custom types implement encoding.TextUnmarshaler.
//
// That means enum-like types such as log.Level can own their parsing/validation logic
// via UnmarshalText.
func textUnmarshalerHook() mapstructure.DecodeHookFunc {
	textUnmarshalerType := reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	stringType := reflect.TypeOf("")

	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String {
			return data, nil
		}

		targetType := to
		if targetType.Kind() != reflect.Pointer {
			targetType = reflect.PointerTo(targetType)
		}

		if !targetType.Implements(textUnmarshalerType) {
			return data, nil
		}

		value, ok := stringValue(data, stringType)
		if !ok {
			return data, nil
		}

		target := reflect.New(to)
		unmarshaler, ok := target.Interface().(encoding.TextUnmarshaler)
		if !ok {
			return data, nil
		}

		if err := unmarshaler.UnmarshalText([]byte(value)); err != nil {
			return nil, err
		}

		if to.Kind() == reflect.Pointer {
			return target.Interface(), nil
		}

		return target.Elem().Interface(), nil
	}
}

func stringToTimeHook(layout string) mapstructure.DecodeHookFunc {
	stringType := reflect.TypeOf("")

	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String {
			return data, nil
		}

		if to != reflect.TypeOf(time.Time{}) {
			return data, nil
		}

		value, ok := stringValue(data, stringType)
		if !ok {
			return data, nil
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return time.Time{}, nil
		}

		parsed, err := time.Parse(layout, value)
		if err != nil {
			return nil, oops.Errorf("parse time %q using layout %q: %w", value, layout, err)
		}

		return parsed, nil
	}
}

func stringToLocationHook() mapstructure.DecodeHookFunc {
	locationType := reflect.TypeOf((*time.Location)(nil))
	stringType := reflect.TypeOf("")

	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to != locationType {
			return data, nil
		}

		value, ok := stringValue(data, stringType)
		if !ok {
			return data, nil
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return time.UTC, nil
		}

		loc, err := time.LoadLocation(value)
		if err != nil {
			return nil, oops.Errorf("load time location %q: %w", value, err)
		}

		return loc, nil
	}
}

func stringValue(data any, stringType reflect.Type) (string, bool) {
	if value, ok := data.(string); ok {
		return value, true
	}

	rv := reflect.ValueOf(data)
	if !rv.IsValid() || !rv.Type().ConvertibleTo(stringType) {
		return "", false
	}

	return rv.Convert(stringType).String(), true
}
