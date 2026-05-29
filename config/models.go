package config

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/otpki/otpki-pkg/log"
	"github.com/samber/oops"
	pgxUUID "github.com/vgarvardt/pgx-google-uuid/v5"
)

// Config is the root application configuration.
type Config struct {
	App      AppConfig        `mapstructure:"app"`
	Log      LogConfig        `mapstructure:"log"`
	Server   ServerConfig     `mapstructure:"server"`
	Plugins  PluginHostConfig `mapstructure:"plugins"`
	Database DatabaseConfig   `mapstructure:"database"`
	Pulsar   PulsarConfig     `mapstructure:"pulsar"`
	Valkey   ValkeyConfig     `mapstructure:"valkey"`
	Crypto   CryptoConfig     `mapstructure:"crypto"`
	Auth     AuthConfig       `mapstructure:"auth"`
	OTEL     OTELConfig       `mapstructure:"otel"`
}

func DefaultConfig() Config {
	return Config{
		App: AppConfig{
			IssuanceService:   true,
			EnrollmentService: true,
			ValidationService: true,
			AuthService:       true,
			Broker:            true,
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
			Enabled:      true,
			Dir:          "./plugins",
			MigrationDir: "",
			ReattachDir:  "", // could be /tmp/otpki-reattach-config
			ReattachWait: 0,
		},
		Database: DatabaseConfig{
			// Do not provide a default connection string because it prevents users
			// from setting individual fields. URL takes precedence when provided.
			Name:     "otpki",
			User:     "otpki",
			Password: "otpki",
			Schema:   "otpki",
			Host:     "localhost",
			Port:     "5432",
			SSLMode:  "disable",
			Pool: PoolConfig{
				MaxConns:          10,
				MinConns:          1,
				MaxConnLifetime:   time.Hour,
				MaxConnIdleTime:   30 * time.Minute,
				HealthCheckPeriod: time.Minute,
			},
		},
		Pulsar: PulsarConfig{
			Connection: "pulsar://localhost:6650",
			Tenant:     "public",
			Namespace:  "default",
		},
		Valkey: ValkeyConfig{
			Enabled:     true,
			Host:        "localhost",
			Port:        "6379",
			Password:    "",
			DB:          0,
			MaxRetries:  3,
			PoolSize:    10,
			KeyPrefix:   "otpki",
			EnableTLS:   false,
			TLSInsecure: false,
		},
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
			ServiceName:      "OTPKI-All",
			ServiceVersion:   "v0.0.1",
			CollectorAddress: "localhost:4317",
			CollectorMode:    CollectorModeGRPC,
		},
	}
}

// AppConfig controls which application services are enabled.
type AppConfig struct {
	IssuanceService   bool `mapstructure:"issuance_service"`
	EnrollmentService bool `mapstructure:"enrollment_service"`
	ValidationService bool `mapstructure:"validation_service"`
	AuthService       bool `mapstructure:"auth_service"`
	Broker            bool `mapstructure:"broker"` // TODO: move to separate broker config when we have more than 1
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
	Enabled      bool          `mapstructure:"enabled"`
	Dir          string        `mapstructure:"dir"`
	MigrationDir string        `mapstructure:"migration_dir"`
	ReattachDir  string        `mapstructure:"reattach_dir"`
	ReattachWait time.Duration `mapstructure:"reattach_wait"`
}

type PulsarConfig struct {
	Connection string `mapstructure:"connection"`
	Tenant     string `mapstructure:"tenant"`
	Namespace  string `mapstructure:"namespace"`
}

// ValkeyConfig configures Valkey/Redis-backed caching.
// See: https://github.com/redis/go-redis
type ValkeyConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	Host        string `mapstructure:"host"`
	Port        string `mapstructure:"port"`
	Password    string `mapstructure:"password"`
	DB          int    `mapstructure:"db"`
	MaxRetries  int    `mapstructure:"max_retries"`
	PoolSize    int    `mapstructure:"pool_size"`
	KeyPrefix   string `mapstructure:"key_prefix"` // Prefix for all cache keys, e.g. "otpki:", see pkg/keys
	EnableTLS   bool   `mapstructure:"enable_tls"`
	TLSInsecure bool   `mapstructure:"tls_insecure"`
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
	ServiceName      string        `mapstructure:"service_name"`
	ServiceVersion   string        `mapstructure:"service_version"`
	CollectorAddress string        `mapstructure:"collector_address"`
	CollectorMode    CollectorMode `mapstructure:"collector_mode"`
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

type DatabaseConfig struct {
	URL string `mapstructure:"url"`

	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	Name     string `mapstructure:"name"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	SSLMode  string `mapstructure:"ssl_mode"`
	Schema   string `mapstructure:"schema"`

	Pool PoolConfig `mapstructure:"pool"`
}

type PoolConfig struct {
	MaxConns          int32         `mapstructure:"max_conns"`
	MinConns          int32         `mapstructure:"min_conns"`
	MaxConnLifetime   time.Duration `mapstructure:"max_conn_lifetime"`
	MaxConnIdleTime   time.Duration `mapstructure:"max_conn_idle_time"`
	HealthCheckPeriod time.Duration `mapstructure:"health_check_period"`
}

func (c DatabaseConfig) PoolConfig() (*pgxpool.Config, error) {
	dsn := c.URL
	if dsn == "" {
		dsn = c.connectionString()
	}

	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}

	searchPath := c.searchPath()

	pgxCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxUUID.Register(conn.TypeMap())

		_, err := conn.Exec(ctx, "set search_path to "+strings.Join(searchPath, ", "))
		return err
	}

	if pgxCfg.ConnConfig.RuntimeParams == nil {
		pgxCfg.ConnConfig.RuntimeParams = map[string]string{}
	}

	pgxCfg.ConnConfig.RuntimeParams["search_path"] = strings.Join(searchPath, ",")

	applyPoolConfig(pgxCfg, c.Pool)

	return pgxCfg, nil
}

func (c DatabaseConfig) connectionString() string {
	q := url.Values{}
	if c.SSLMode != "" {
		q.Set("sslmode", c.SSLMode)
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   net.JoinHostPort(c.Host, c.Port),
		Path:   "/" + c.Name,
	}

	if encoded := q.Encode(); encoded != "" {
		u.RawQuery = encoded
	}

	return u.String()
}

func (c DatabaseConfig) searchPath() []string {
	if c.Schema == "" {
		return []string{"public"}
	}

	return []string{c.Schema, "public"}
}

func applyPoolConfig(cfg *pgxpool.Config, pool PoolConfig) {
	if pool.MaxConns > 0 {
		cfg.MaxConns = pool.MaxConns
	}

	if pool.MinConns > 0 {
		cfg.MinConns = pool.MinConns
	}

	if pool.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = pool.MaxConnLifetime
	}

	if pool.MaxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = pool.MaxConnIdleTime
	}

	if pool.HealthCheckPeriod > 0 {
		cfg.HealthCheckPeriod = pool.HealthCheckPeriod
	}
}
