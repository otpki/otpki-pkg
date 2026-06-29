package clientcfg

import (
	"context"
	"encoding"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/samber/oops"
	pgxUUID "github.com/vgarvardt/pgx-google-uuid/v5"
)

var _ encoding.TextUnmarshaler = (*PostgresSSLMode)(nil)
var _ encoding.TextUnmarshaler = (*PostgresLogLevel)(nil)

type PostgresSSLMode string

const (
	PostgresSSLDisable    PostgresSSLMode = "disable"
	PostgresSSLAllow      PostgresSSLMode = "allow"
	PostgresSSLPrefer     PostgresSSLMode = "prefer"
	PostgresSSLRequire    PostgresSSLMode = "require"
	PostgresSSLVerifyCA   PostgresSSLMode = "verify-ca"
	PostgresSSLVerifyFull PostgresSSLMode = "verify-full"
)

func (m *PostgresSSLMode) UnmarshalText(text []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(text))) {
	case "", "prefer":
		*m = PostgresSSLPrefer
	case "disable":
		*m = PostgresSSLDisable
	case "allow":
		*m = PostgresSSLAllow
	case "require":
		*m = PostgresSSLRequire
	case "verify-ca", "verify_ca", "verifyca":
		*m = PostgresSSLVerifyCA
	case "verify-full", "verify_full", "verifyfull":
		*m = PostgresSSLVerifyFull
	default:
		return oops.Errorf("invalid postgres sslmode %q", string(text))
	}
	return nil
}

type PostgresLogLevel string

const (
	PostgresLogLevelTrace PostgresLogLevel = "trace"
	PostgresLogLevelDebug PostgresLogLevel = "debug"
	PostgresLogLevelInfo  PostgresLogLevel = "info"
	PostgresLogLevelWarn  PostgresLogLevel = "warn"
	PostgresLogLevelError PostgresLogLevel = "error"
)

func (l *PostgresLogLevel) UnmarshalText(text []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(text))) {
	case "", "error":
		*l = PostgresLogLevelError
	case "trace":
		*l = PostgresLogLevelTrace
	case "debug":
		*l = PostgresLogLevelDebug
	case "info":
		*l = PostgresLogLevelInfo
	case "warn", "warning":
		*l = PostgresLogLevelWarn
	default:
		return oops.Errorf("invalid postgres log level %q", string(text))
	}
	return nil
}

func (l PostgresLogLevel) tracelogLevel() tracelog.LogLevel {
	switch l {
	case PostgresLogLevelTrace:
		return tracelog.LogLevelTrace
	case PostgresLogLevelDebug:
		return tracelog.LogLevelDebug
	case PostgresLogLevelInfo:
		return tracelog.LogLevelInfo
	case PostgresLogLevelWarn:
		return tracelog.LogLevelWarn
	default:
		return tracelog.LogLevelError
	}
}

type PostgresClientConfig struct {
	URL                      string            `mapstructure:"url"`
	Host                     string            `mapstructure:"host"`
	Port                     uint16            `mapstructure:"port"`
	Database                 string            `mapstructure:"database"`
	User                     string            `mapstructure:"user"`
	Password                 string            `mapstructure:"password"`
	SSLMode                  PostgresSSLMode   `mapstructure:"ssl_mode"`
	Schema                   string            `mapstructure:"schema"`
	ConnectTimeout           time.Duration     `mapstructure:"connect_timeout"`
	ApplicationName          string            `mapstructure:"application_name"`
	RuntimeParams            map[string]string `mapstructure:"runtime_params"`
	TLS                      *TLSConfig        `mapstructure:"tls"`
	StatementCacheCapacity   int               `mapstructure:"statement_cache_capacity"`
	DescriptionCacheCapacity int               `mapstructure:"description_cache_capacity"`
	MaxConns                 int32             `mapstructure:"max_conns"`
	MinConns                 int32             `mapstructure:"min_conns"`
	MaxConnLifetime          time.Duration     `mapstructure:"max_conn_lifetime"`
	MaxConnIdleTime          time.Duration     `mapstructure:"max_conn_idle_time"`
	HealthCheckPeriod        time.Duration     `mapstructure:"health_check_period"`
	LogLevel                 PostgresLogLevel  `mapstructure:"log_level"`
}

func DefaultPostgresClientConfig() PostgresClientConfig {
	return PostgresClientConfig{
		Host:              "localhost",
		Port:              5432,
		Database:          "otpki",
		User:              "otpki",
		Password:          "otpki",
		SSLMode:           PostgresSSLDisable,
		Schema:            "otpki",
		MaxConns:          10,
		MinConns:          1,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
}

func NewPostgresPool(ctx context.Context, cfg PostgresClientConfig, logger *slog.Logger) (*pgxpool.Pool, error) {
	defaults := DefaultPostgresClientConfig()
	if cfg.Host == "" && cfg.URL == "" {
		cfg.Host = defaults.Host
	}
	if cfg.Port == 0 && cfg.URL == "" {
		cfg.Port = defaults.Port
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = defaults.SSLMode
	}

	poolCfg, err := pgxpool.ParseConfig(postgresConnString(cfg))
	if err != nil {
		return nil, err
	}
	applyPostgresConnBehavior(poolCfg.ConnConfig, cfg, logger)
	prevAfterConnect := poolCfg.AfterConnect
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxUUID.Register(conn.TypeMap())
		if prevAfterConnect != nil {
			if err := prevAfterConnect(ctx, conn); err != nil {
				return err
			}
		}
		return nil
		//_, err := conn.Exec(ctx, "set search_path to "+strings.Join(searchPath, ", "))
		//return err
	}
	if cfg.TLS != nil {
		tlsCfg, err := BuildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, err
		}
		poolCfg.ConnConfig.TLSConfig = tlsCfg
	}
	if cfg.StatementCacheCapacity > 0 {
		poolCfg.ConnConfig.StatementCacheCapacity = cfg.StatementCacheCapacity
	}
	if cfg.DescriptionCacheCapacity > 0 {
		poolCfg.ConnConfig.DescriptionCacheCapacity = cfg.DescriptionCacheCapacity
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	}
	return pgxpool.NewWithConfig(ctx, poolCfg)
}

func postgresConnString(cfg PostgresClientConfig) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), Path: cfg.Database}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := u.Query()
	if cfg.SSLMode != "" {
		q.Set("sslmode", string(cfg.SSLMode))
	}
	if cfg.ConnectTimeout > 0 {
		q.Set("connect_timeout", strconv.Itoa(int(cfg.ConnectTimeout.Seconds())))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func NewPostgresConnConfig(cfg PostgresClientConfig, logger *slog.Logger) (*pgx.ConnConfig, error) {
	poolCfg, err := pgxpool.ParseConfig(postgresConnString(cfg))
	if err != nil {
		return nil, err
	}
	applyPostgresConnBehavior(poolCfg.ConnConfig, cfg, logger)
	return poolCfg.ConnConfig, nil
}

func applyPostgresConnBehavior(connCfg *pgx.ConnConfig, cfg PostgresClientConfig, logger *slog.Logger) {
	if connCfg == nil {
		return
	}
	searchPath := postgresSearchPath(cfg)

	if connCfg.RuntimeParams == nil {
		connCfg.RuntimeParams = map[string]string{}
	}
	connCfg.RuntimeParams["search_path"] = strings.Join(searchPath, ",")
	if cfg.ApplicationName != "" {
		connCfg.RuntimeParams["application_name"] = cfg.ApplicationName
	}
	for k, v := range cfg.RuntimeParams {
		connCfg.RuntimeParams[k] = v
	}
	if logger != nil {
		connCfg.Tracer = &tracelog.TraceLog{Logger: NewPGXLogger(logger), LogLevel: cfg.LogLevel.tracelogLevel()}
	}
}

func postgresSearchPath(cfg PostgresClientConfig) []string {
	if cfg.Schema == "" {
		return []string{"public"}
	}
	return []string{cfg.Schema, "public"}
}
