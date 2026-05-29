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

	DefaultConfigFile = "config.yaml"
)

type LoadOptions struct {
	// Values are arbitrary in-memory config values, usually from the operator CRD.
	Values map[string]any

	// Env contains env-style overrides, e.g. OTPKI_APP_ISSUANCESERVICE=true.
	Env map[string]string

	// ReadProcessEnv enables reading os.Environ().
	ReadProcessEnv bool

	// ReadConfigFile enables reading a config file from env/config options.
	ReadConfigFile bool

	// ConfigFile optionally overrides OTPKI_CONFIG_FILE.
	ConfigFile string

	// ConfigType optionally overrides OTPKI_CONFIG_TYPE.
	ConfigType string
}

func LoadConfig() (Config, error) {
	return LoadConfigWithOptions(LoadOptions{
		ReadProcessEnv: true,
		ReadConfigFile: true,
	})
}

func LoadConfigFromMap(values map[string]any) (Config, error) {
	return LoadConfigWithOptions(LoadOptions{
		Values: values,
	})
}

func LoadConfigWithOptions(opts LoadOptions) (Config, error) {
	v := viper.New()

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaultsFromStruct(v, "", DefaultConfig())

	if opts.Values != nil {
		if err := mergeConfigMap(v, opts.Values); err != nil {
			return Config{}, err
		}
	}

	for name, value := range opts.Env {
		configKey, ok := envNameToConfigKey(name)
		if !ok {
			continue
		}
		v.Set(configKey, value)
	}

	if opts.ReadProcessEnv {
		v.AutomaticEnv()
		bindDynamicPluginEnv(v)
	}

	if opts.ReadConfigFile {
		if err := readConfigFileWithOptions(v, opts); err != nil {
			return Config{}, err
		}
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

func mergeConfigMap(v *viper.Viper, values map[string]any) error {
	flat := map[string]any{}
	flattenMap("", values, flat)

	for key, value := range flat {
		v.Set(key, value)
	}

	return nil
}

func flattenMap(prefix string, in map[string]any, out map[string]any) {
	for key, value := range in {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		if nested, ok := value.(map[string]any); ok {
			flattenMap(fullKey, nested, out)
			continue
		}

		out[fullKey] = value
	}
}

func envNameToConfigKey(name string) (string, bool) {
	if !strings.HasPrefix(name, EnvPrefix+"_") {
		return "", false
	}

	key := strings.TrimPrefix(name, EnvPrefix+"_")
	if key == "" {
		return "", false
	}

	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", ".")
	return key, true
}

func readConfigFileWithOptions(v *viper.Viper, opts LoadOptions) error {
	configFile := strings.TrimSpace(opts.ConfigFile)
	if configFile == "" {
		configFile = strings.TrimSpace(os.Getenv(EnvConfigFile))
	}
	if configFile == "" {
		configFile = DefaultConfigFile
	}

	if _, err := os.Stat(configFile); err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return oops.Errorf("failed to stat config file %q: %w", configFile, err)
	}

	v.SetConfigFile(configFile)

	if filepath.Ext(configFile) == "" {
		configType := strings.TrimSpace(opts.ConfigType)
		if configType == "" {
			configType = strings.TrimSpace(os.Getenv(EnvConfigType))
		}
		if configType == "" {
			return oops.Errorf(
				"%s is extensionless; set %s to yaml, json, toml, hcl, ini, env, or properties",
				configFile,
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
