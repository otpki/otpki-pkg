package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/samber/oops"
	"github.com/spf13/viper"
)

const (
	EnvPrefix     = "OTPKI"
	EnvConfigFile = EnvPrefix + "_CONFIG_FILE"
	EnvConfigType = EnvPrefix + "_CONFIG_TYPE" // optional fallback for extensionless files
)

func LoadConfig() (Config, error) {
	v := viper.New()

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaultsFromStruct(v, "", DefaultConfig())

	if err := readConfigFileIfProvided(v); err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := v.Unmarshal(
		&cfg,
		viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
			stringToLocationHook(),
			stringToTimeHook(time.RFC3339),
			mapstructure.StringToSliceHookFunc(","),
			mapstructure.StringToTimeDurationHookFunc(),
			textUnmarshalerHook(),
		)),
	); err != nil {
		return Config{}, oops.Errorf("failed to unmarshal config: %w", err)
	}

	// TODO: validate config

	return cfg, nil
}

func readConfigFileIfProvided(v *viper.Viper) error {
	configFile := strings.TrimSpace(os.Getenv(EnvConfigFile))
	if configFile == "" {
		return nil
	}

	v.SetConfigFile(configFile)

	// needed only for extensionless config files (.bashrc, for example)
	if filepath.Ext(configFile) == "" {
		configType := strings.TrimSpace(os.Getenv(EnvConfigType))
		if configType == "" {
			return oops.Errorf(
				"%s is set to an extensionless file; set %s to yaml, json, toml, hcl, ini, env, or properties",
				EnvConfigFile,
				EnvConfigType,
			)
		}

		v.SetConfigType(configType)
	}

	if err := v.ReadInConfig(); err != nil {
		return oops.Errorf("failed to read config file %q: %w", configFile, err)
	}

	return nil
}

func setDefaultsFromStruct(v *viper.Viper, prefix string, s any) {
	walkDefaults(v, prefix, reflect.ValueOf(s))
}

func walkDefaults(v *viper.Viper, prefix string, rv reflect.Value) {
	if !rv.IsValid() {
		return
	}

	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}

	rt := rv.Type()

	if isConfigLeafType(rt) {
		v.SetDefault(prefix, rv.Interface())
		return
	}

	if rv.Kind() != reflect.Struct {
		if prefix != "" {
			v.SetDefault(prefix, rv.Interface())
		}
		return
	}

	for i := 0; i < rv.NumField(); i++ {
		field := rt.Field(i)
		value := rv.Field(i)

		if !field.IsExported() {
			continue
		}

		key := mapstructureFieldName(field)
		if key == "-" {
			continue
		}

		if key == "" {
			key = strings.ToLower(field.Name)
		}

		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		walkDefaults(v, fullKey, value)
	}
}

func mapstructureFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("mapstructure")
	if tag == "" {
		return ""
	}

	name := strings.Split(tag, ",")[0]
	return strings.TrimSpace(name)
}

func isConfigLeafType(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t == reflect.TypeOf(time.Time{}) {
		return true
	}

	if t == reflect.TypeOf(time.Duration(0)) {
		return true
	}

	if t == reflect.TypeOf(time.Location{}) {
		return true
	}

	switch t.Kind() {
	case reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Float32,
		reflect.Float64,
		reflect.String,
		reflect.Slice,
		reflect.Array,
		reflect.Map:
		return true
	default:
		return false
	}
}

func bindDynamicPluginEnv(v *viper.Viper) {
	const (
		envPrefix = EnvPrefix + "_PLUGINS_"
		envSuffix = "_SCHEMA"
	)

	for _, item := range os.Environ() {
		name, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}

		if !strings.HasPrefix(name, envPrefix) || !strings.HasSuffix(name, envSuffix) {
			continue
		}

		rawPluginName := strings.TrimSuffix(strings.TrimPrefix(name, envPrefix), envSuffix)
		if rawPluginName == "" {
			continue
		}

		pluginKey := normalizePluginConfigKey(rawPluginName)
		configKey := "plugins." + pluginKey + ".schema"

		// registers the dynamic key so Unmarshal can see it.
		_ = v.BindEnv(configKey, name)
	}
}

func normalizePluginConfigKey(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "_")
	return name
}
