package clientcfg

import (
	"crypto/tls"
	"log/slog"
	"time"

	redis "github.com/redis/go-redis/v9"
)

type RedisClientConfig struct {
	Addrs                 []string      `mapstructure:"addrs"`
	MasterName            string        `mapstructure:"master_name"`
	ClientName            string        `mapstructure:"client_name"`
	DB                    int           `mapstructure:"db"`
	Protocol              int           `mapstructure:"protocol"`
	Username              string        `mapstructure:"username"`
	Password              string        `mapstructure:"password"`
	SentinelUsername      string        `mapstructure:"sentinel_username"`
	SentinelPassword      string        `mapstructure:"sentinel_password"`
	MaxRetries            int           `mapstructure:"max_retries"`
	MinRetryBackoff       time.Duration `mapstructure:"min_retry_backoff"`
	MaxRetryBackoff       time.Duration `mapstructure:"max_retry_backoff"`
	DialTimeout           time.Duration `mapstructure:"dial_timeout"`
	DialerRetries         int           `mapstructure:"dialer_retries"`
	DialerRetryTimeout    time.Duration `mapstructure:"dialer_retry_timeout"`
	ReadTimeout           time.Duration `mapstructure:"read_timeout"`
	WriteTimeout          time.Duration `mapstructure:"write_timeout"`
	ContextTimeoutEnabled bool          `mapstructure:"context_timeout_enabled"`
	ReadBufferSize        int           `mapstructure:"read_buffer_size"`
	WriteBufferSize       int           `mapstructure:"write_buffer_size"`
	PoolFIFO              bool          `mapstructure:"pool_fifo"`
	PoolSize              int           `mapstructure:"pool_size"`
	MaxConcurrentDials    int           `mapstructure:"max_concurrent_dials"`
	PoolTimeout           time.Duration `mapstructure:"pool_timeout"`
	MinIdleConns          int           `mapstructure:"min_idle_conns"`
	MaxIdleConns          int           `mapstructure:"max_idle_conns"`
	MaxActiveConns        int           `mapstructure:"max_active_conns"`
	ConnMaxIdleTime       time.Duration `mapstructure:"conn_max_idle_time"`
	ConnMaxLifetime       time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxLifetimeJitter time.Duration `mapstructure:"conn_max_lifetime_jitter"`
	TLS                   *TLSConfig    `mapstructure:"tls"`
	MaxRedirects          int           `mapstructure:"max_redirects"`
	ReadOnly              bool          `mapstructure:"read_only"`
	RouteByLatency        bool          `mapstructure:"route_by_latency"`
	RouteRandomly         bool          `mapstructure:"route_randomly"`
	DisableIdentity       bool          `mapstructure:"disable_identity"`
	IdentitySuffix        string        `mapstructure:"identity_suffix"`
	IsClusterMode         bool          `mapstructure:"is_cluster_mode"`
}

func DefaultRedisClientConfig() RedisClientConfig {
	return RedisClientConfig{
		Addrs:           []string{"localhost:6379"},
		DB:              0,
		MaxRetries:      0,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolSize:        10,
		PoolTimeout:     4 * time.Second,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

func NewRedisClient(cfg RedisClientConfig, logger *slog.Logger) (redis.UniversalClient, error) {
	defaults := DefaultRedisClientConfig()
	if len(cfg.Addrs) == 0 {
		cfg.Addrs = defaults.Addrs
	}
	if logger != nil {
		if redisLogger := NewRedisLogger(logger); redisLogger != nil {
			redis.SetLogger(redisLogger)
		}
	}
	var tlsCfg *tls.Config
	if cfg.TLS != nil {
		var err error
		tlsCfg, err = BuildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, err
		}
	}
	return redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:                 append([]string(nil), cfg.Addrs...),
		MasterName:            cfg.MasterName,
		ClientName:            cfg.ClientName,
		DB:                    cfg.DB,
		Protocol:              cfg.Protocol,
		Username:              cfg.Username,
		Password:              cfg.Password,
		SentinelUsername:      cfg.SentinelUsername,
		SentinelPassword:      cfg.SentinelPassword,
		MaxRetries:            cfg.MaxRetries,
		MinRetryBackoff:       cfg.MinRetryBackoff,
		MaxRetryBackoff:       cfg.MaxRetryBackoff,
		DialTimeout:           cfg.DialTimeout,
		DialerRetries:         cfg.DialerRetries,
		DialerRetryTimeout:    cfg.DialerRetryTimeout,
		ReadTimeout:           cfg.ReadTimeout,
		WriteTimeout:          cfg.WriteTimeout,
		ContextTimeoutEnabled: cfg.ContextTimeoutEnabled,
		ReadBufferSize:        cfg.ReadBufferSize,
		WriteBufferSize:       cfg.WriteBufferSize,
		PoolFIFO:              cfg.PoolFIFO,
		PoolSize:              cfg.PoolSize,
		MaxConcurrentDials:    cfg.MaxConcurrentDials,
		PoolTimeout:           cfg.PoolTimeout,
		MinIdleConns:          cfg.MinIdleConns,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxActiveConns:        cfg.MaxActiveConns,
		ConnMaxIdleTime:       cfg.ConnMaxIdleTime,
		ConnMaxLifetime:       cfg.ConnMaxLifetime,
		ConnMaxLifetimeJitter: cfg.ConnMaxLifetimeJitter,
		TLSConfig:             tlsCfg,
		MaxRedirects:          cfg.MaxRedirects,
		ReadOnly:              cfg.ReadOnly,
		RouteByLatency:        cfg.RouteByLatency,
		RouteRandomly:         cfg.RouteRandomly,
		DisableIdentity:       cfg.DisableIdentity,
		IdentitySuffix:        cfg.IdentitySuffix,
		IsClusterMode:         cfg.IsClusterMode,
	}), nil
}
