package config

import (
	"context"
	"os"
	"strings"

	"github.com/samber/oops"
	"github.com/spf13/viper"
)

type Loader[T any] interface {
	Load(ctx context.Context) (map[string]any, error)
}

type EnvironmentLoader struct {
	Prefix string // Prefix for environment variables ("OTPKI")
}

func NewEnvironmentLoader(prefix string) *EnvironmentLoader {
	return &EnvironmentLoader{
		Prefix: prefix,
	}
}

func (l *EnvironmentLoader) Load(_ context.Context) (map[string]any, error) {
	v := viper.New()

	// configure viper to read environment variables
	if l.Prefix != "" {
		v.SetEnvPrefix(l.Prefix)
	}
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// scan environment variables with our prefix to discover keys
	// this is needed because viper's AutomaticEnv() only checks known keys
	fullPrefix := l.Prefix
	if fullPrefix != "" && !strings.HasSuffix(fullPrefix, "_") {
		fullPrefix += "_"
	}

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]

		// check if this env var has our prefix
		if fullPrefix != "" && !strings.HasPrefix(key, fullPrefix) {
			continue
		}

		// remove the prefix and convert to config key format
		// OTPKI_DATABASE_HOST -> DATABASE_HOST -> database.host
		configKey := strings.TrimPrefix(key, fullPrefix)
		if configKey == "" {
			continue
		}
		configKey = strings.ToLower(strings.ReplaceAll(configKey, "_", "."))

		// tell viper about this key so it will read the env var
		_ = v.BindEnv(configKey)
	}

	return v.AllSettings(), nil
}

type FileLoader struct {
	Path string
}

func NewFileLoader(path string) *FileLoader {
	return &FileLoader{
		Path: path,
	}
}

func (l *FileLoader) Load(_ context.Context) (map[string]any, error) {
	v := viper.New()
	v.SetConfigFile(l.Path)

	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return nil, oops.Code("source_not_found").Errorf("config file not found: %w", err)
		}
		return nil, oops.Code("source_not_found").Errorf("failed to read config file: %w", err)
	}

	return v.AllSettings(), nil
}

type MapLoader struct {
	Data map[string]any
}

func NewMapLoader(data map[string]any) *MapLoader {
	return &MapLoader{Data: data}
}

func (l *MapLoader) Load(_ context.Context) (map[string]any, error) {
	return l.Data, nil
}
