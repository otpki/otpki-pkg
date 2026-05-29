package config

import (
	"strings"
	"time"

	"github.com/otpki/otpki-pkg/log"
	"github.com/samber/oops"
)

// Config is the root application configuration.
type Config struct {
	App        AppConfig        `mapstructure:"app"`
	Log        LogConfig        `mapstructure:"log"`
	Server     ServerConfig     `mapstructure:"server"`
	Plugins    PluginHostConfig `mapstructure:"plugins"`
	Backends   BackendsConfig   `mapstructure:"backends"`
	StateStore StateStoreConfig `mapstructure:"statestore"`
	PubSub     PubSubConfig     `mapstructure:"pubsub"`
	Crypto     CryptoConfig     `mapstructure:"crypto"`
	Auth       AuthConfig       `mapstructure:"auth"`
	OTEL       OTELConfig       `mapstructure:"otel"`
}

func DefaultConfig() Config {
	return Config{
		App: AppConfig{
			IssuanceService:   true,
			EnrollmentService: true,
			ValidationService: true,
			Keyspace: KeyspaceConfig{
				App:      "otpki",
				TenantID: "default",
			},
		},
		Log: LogConfig{
			Level: log.DebugLevel,
		},
		Server: ServerConfig{
			HTTPListener:   ":15580",
			AllowedOrigins: []string{"*"},
			EnableWebUI:    true,
		},
		Plugins: PluginHostConfig{
			Enabled: true,
			Dir:     "./plugins",
		},
		Backends:   DefaultBackendsConfig(),
		StateStore: DefaultStateStoreConfig(),
		PubSub:     DefaultPubSubConfig(),
		Crypto: CryptoConfig{
			EncryptionPassword: "",
		},
		Auth: AuthConfig{
			Enabled:                 true,
			ClientCertificateHeader: "X-Client-Certificate",
			DefaultProvider: AuthDefaultProvider{
				Name:               "internal",
				ClientId:           "otpki-service",
				ClientSecret:       "dev-service-secret-change-me",
				IssuerUrl:          "https://pki.localhost/kc/realms/otpki",
				RedirectUrl:        "https://localhost:5173/auth/callback",
				PostLogoutUrl:      "/auth/login/internal",
				Scopes:             []string{"openid"},
				MaxSkew:            5 * time.Minute,
				SessionMaxInactive: 2 * time.Hour,
				SessionMaxLength:   24 * time.Hour,
				CreateUnknownUsers: true,
				CreateUnknownRoles: true,
			},
		},
		OTEL: OTELConfig{
			Enabled:          true,
			ServiceName:      "OTPKI-All",
			ServiceVersion:   "v0.0.1",
			CollectorAddress: "localhost:4317",
			CollectorMode:    CollectorModeGRPC,
		},
	}
}

type CollectorMode string

const (
	CollectorModeGRPC CollectorMode = "grpc"
	CollectorModeHTTP CollectorMode = "http"
)

func (c *CollectorMode) UnmarshalText(text []byte) error {
	value := CollectorMode(text)

	switch value {
	case CollectorModeGRPC, CollectorModeHTTP:
		*c = value
		return nil
	default:
		return oops.Errorf("invalid collector mode %q", string(text))
	}
}

type RedisType string

const (
	RedisTypeNode    RedisType = "node"
	RedisTypeCluster RedisType = "cluster"
)

func (r *RedisType) UnmarshalText(text []byte) error {
	value := RedisType(text)

	switch value {
	case RedisTypeNode, RedisTypeCluster:
		*r = value
		return nil
	default:
		return oops.Errorf("invalid state provider %q", string(text))
	}
}

// AppConfig controls which application services are enabled.
type AppConfig struct {
	IssuanceService   bool           `mapstructure:"issuance_service"`
	EnrollmentService bool           `mapstructure:"enrollment_service"`
	ValidationService bool           `mapstructure:"validation_service"`
	Keyspace          KeyspaceConfig `mapstructure:"keyspace"`
}

// KeyspaceConfig is used to create cache keys
type KeyspaceConfig struct {
	App       string `mapstructure:"app"`
	Env       string `mapstructure:"env"`
	TestRunID string `mapstructure:"test_run_id"`
	TenantID  string `mapstructure:"tenant_id"`
}

func (k KeyspaceConfig) Prefix() string {
	parts := []string{k.App}

	if k.TestRunID != "" {
		parts = append(parts, "test", k.TestRunID)
	} else if k.Env != "" {
		parts = append(parts, k.Env)
	}

	if k.TenantID != "" {
		parts = append(parts, "tenant", k.TenantID)
	} else {
		parts = append(parts, "tenant", "default")
	}

	return strings.Join(parts, "/")
}

func (k KeyspaceConfig) Key(parts ...string) string {
	all := append([]string{k.Prefix()}, parts...)
	return strings.Join(all, "/")
}

type LogConfig struct {
	Level log.Level `mapstructure:"level"`
}

type ServerConfig struct {
	HTTPListener   string   `mapstructure:"http_listener"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	EnableWebUI    bool     `mapstructure:"enable_web_ui"`
}

type PluginHostConfig struct {
	Enabled             bool                           `mapstructure:"enabled"`
	Dir                 string                         `mapstructure:"dir"`
	ReattachDir         string                         `mapstructure:"reattach_dir"` // could be /tmp/otpki-reattach-config
	ReattachWait        time.Duration                  `mapstructure:"reattach_wait"`
	ExportMigrations    bool                           `mapstructure:"export_migrations"`
	MigrationDir        string                         `mapstructure:"migration_dir"`
	MigrationConfigMaps MigrationConfigMapExportConfig `mapstructure:"migration_config_maps"`
}

type MigrationConfigMapExportConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	Namespace  string `mapstructure:"namespace"`   // Namespace defaults to the pod namespace if empty.
	Instance   string `mapstructure:"instance"`    // Instance is used as a label selector value, usually the OmniTrustPKI name.
	Prefix     string `mapstructure:"prefix"`      // Prefix defaults to "<instance>-migrations" if empty.
	SourceHash string `mapstructure:"source_hash"` // SourceHash labels generated migration ConfigMaps so the operator can
	// distinguish the current app/plugin migration set from stale ones.
}

type CryptoConfig struct {
	EncryptionPassword string `mapstructure:"encryption_password"`
}

type AuthConfig struct {
	Enabled                 bool                `mapstructure:"enabled"`
	ClientCertificateHeader string              `mapstructure:"client_certificate_header"`
	DefaultProvider         AuthDefaultProvider `mapstructure:"default_provider"`
}

type AuthDefaultProvider struct {
	Name               string        `mapstructure:"name"`
	ClientId           string        `mapstructure:"client_id"`
	ClientSecret       string        `mapstructure:"client_secret"`
	IssuerUrl          string        `mapstructure:"issuer_url"`
	BackchannelUrl     string        `mapstructure:"backchannel_url"`
	RedirectUrl        string        `mapstructure:"redirect_url"`
	PostLogoutUrl      string        `mapstructure:"post_logout_url"`
	Scopes             []string      `mapstructure:"scopes"`
	MaxSkew            time.Duration `mapstructure:"max_skew"`
	SessionMaxInactive time.Duration `mapstructure:"session_max_inactive"`
	SessionMaxLength   time.Duration `mapstructure:"session_max_length"`
	CreateUnknownUsers bool          `mapstructure:"create_unknown_users"`
	CreateUnknownRoles bool          `mapstructure:"create_unknown_roles"`
}

type OTELConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	ServiceName      string        `mapstructure:"service_name"`
	ServiceVersion   string        `mapstructure:"service_version"`
	CollectorAddress string        `mapstructure:"collector_address"`
	CollectorMode    CollectorMode `mapstructure:"collector_mode"`
}
