package clientcfg

import (
	"log/slog"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

type NATSClientConfig struct {
	URL                     string        `mapstructure:"url"`
	URLs                    []string      `mapstructure:"urls"`
	Name                    string        `mapstructure:"name"`
	Token                   string        `mapstructure:"token"`
	Username                string        `mapstructure:"username"`
	Password                string        `mapstructure:"password"`
	CredentialsFile         string        `mapstructure:"credentials_file"`
	CredentialsSeedFile     string        `mapstructure:"credentials_seed_file"`
	NKeySeedFile            string        `mapstructure:"nkey_seed_file"`
	UserJWT                 string        `mapstructure:"user_jwt"`
	UserSeed                string        `mapstructure:"user_seed"`
	InboxPrefix             string        `mapstructure:"inbox_prefix"`
	NoEcho                  bool          `mapstructure:"no_echo"`
	NoRandomize             bool          `mapstructure:"no_randomize"`
	AllowReconnect          bool          `mapstructure:"allow_reconnect"`
	MaxReconnect            int           `mapstructure:"max_reconnect"`
	ReconnectWait           time.Duration `mapstructure:"reconnect_wait"`
	ReconnectJitter         time.Duration `mapstructure:"reconnect_jitter"`
	ReconnectJitterTLS      time.Duration `mapstructure:"reconnect_jitter_tls"`
	Timeout                 time.Duration `mapstructure:"timeout"`
	DrainTimeout            time.Duration `mapstructure:"drain_timeout"`
	FlusherTimeout          time.Duration `mapstructure:"flusher_timeout"`
	ReconnectBufSize        int           `mapstructure:"reconnect_buf_size"`
	ReconnectOnFlusherError bool          `mapstructure:"reconnect_on_flusher_error"`
	PingInterval            time.Duration `mapstructure:"ping_interval"`
	MaxPingsOut             int           `mapstructure:"max_pings_out"`
	RetryOnFailedConnect    bool          `mapstructure:"retry_on_failed_connect"`
	TLSHandshakeFirst       bool          `mapstructure:"tls_handshake_first"`
	SubChanLen              int           `mapstructure:"sub_chan_len"`
	WriteBufferSize         int           `mapstructure:"write_buffer_size"`
	TLS                     *TLSConfig    `mapstructure:"tls"`
}

func DefaultNATSClientConfig() NATSClientConfig {
	return NATSClientConfig{
		URL:                natsgo.DefaultURL,
		Name:               "spfy",
		AllowReconnect:     true,
		MaxReconnect:       60,
		ReconnectWait:      2 * time.Second,
		ReconnectJitter:    100 * time.Millisecond,
		ReconnectJitterTLS: time.Second,
		Timeout:            2 * time.Second,
		DrainTimeout:       30 * time.Second,
		FlusherTimeout:     time.Minute,
		PingInterval:       2 * time.Minute,
		MaxPingsOut:        2,
		ReconnectBufSize:   8 * 1024 * 1024,
		SubChanLen:         65536,
		WriteBufferSize:    32768,
	}
}

func NewNATSClient(cfg NATSClientConfig, logger *slog.Logger) (*natsgo.Conn, error) {
	defaults := DefaultNATSClientConfig()
	if cfg.URL == "" && len(cfg.URLs) == 0 {
		cfg.URL = defaults.URL
	}
	cfg.AllowReconnect = cfg.AllowReconnect || defaults.AllowReconnect
	if cfg.MaxReconnect == 0 {
		cfg.MaxReconnect = defaults.MaxReconnect
	}
	if cfg.ReconnectWait == 0 {
		cfg.ReconnectWait = defaults.ReconnectWait
	}
	if cfg.ReconnectJitter == 0 {
		cfg.ReconnectJitter = defaults.ReconnectJitter
	}
	if cfg.ReconnectJitterTLS == 0 {
		cfg.ReconnectJitterTLS = defaults.ReconnectJitterTLS
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.DrainTimeout == 0 {
		cfg.DrainTimeout = defaults.DrainTimeout
	}
	if cfg.FlusherTimeout == 0 {
		cfg.FlusherTimeout = defaults.FlusherTimeout
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = defaults.PingInterval
	}
	if cfg.MaxPingsOut == 0 {
		cfg.MaxPingsOut = defaults.MaxPingsOut
	}
	if cfg.ReconnectBufSize == 0 {
		cfg.ReconnectBufSize = defaults.ReconnectBufSize
	}
	if cfg.SubChanLen == 0 {
		cfg.SubChanLen = defaults.SubChanLen
	}
	if cfg.WriteBufferSize == 0 {
		cfg.WriteBufferSize = defaults.WriteBufferSize
	}
	if cfg.Name == "" {
		cfg.Name = defaults.Name
	}

	opts := natsgo.GetDefaultOptions()
	opts.Url = cfg.URL
	if len(cfg.URLs) > 0 {
		opts.Servers = append([]string(nil), cfg.URLs...)
	}
	opts.Name = cfg.Name
	opts.Token = cfg.Token
	opts.User = cfg.Username
	opts.Password = cfg.Password
	opts.InboxPrefix = cfg.InboxPrefix
	opts.NoEcho = cfg.NoEcho
	opts.NoRandomize = cfg.NoRandomize
	opts.AllowReconnect = cfg.AllowReconnect
	if cfg.MaxReconnect != 0 {
		opts.MaxReconnect = cfg.MaxReconnect
	}
	if cfg.ReconnectWait > 0 {
		opts.ReconnectWait = cfg.ReconnectWait
	}
	if cfg.ReconnectJitter > 0 {
		opts.ReconnectJitter = cfg.ReconnectJitter
	}
	if cfg.ReconnectJitterTLS > 0 {
		opts.ReconnectJitterTLS = cfg.ReconnectJitterTLS
	}
	if cfg.Timeout > 0 {
		opts.Timeout = cfg.Timeout
	}
	if cfg.DrainTimeout > 0 {
		opts.DrainTimeout = cfg.DrainTimeout
	}
	if cfg.FlusherTimeout > 0 {
		opts.FlusherTimeout = cfg.FlusherTimeout
	}
	if cfg.ReconnectBufSize > 0 {
		opts.ReconnectBufSize = cfg.ReconnectBufSize
	}
	opts.ReconnectOnFlusherError = cfg.ReconnectOnFlusherError
	if cfg.PingInterval > 0 {
		opts.PingInterval = cfg.PingInterval
	}
	if cfg.MaxPingsOut > 0 {
		opts.MaxPingsOut = cfg.MaxPingsOut
	}
	opts.RetryOnFailedConnect = cfg.RetryOnFailedConnect
	opts.TLSHandshakeFirst = cfg.TLSHandshakeFirst
	if cfg.SubChanLen > 0 {
		opts.SubChanLen = cfg.SubChanLen
	}
	if cfg.WriteBufferSize > 0 {
		opts.WriteBufferSize = cfg.WriteBufferSize
	}
	if cfg.TLS != nil {
		tlsCfg, err := BuildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, err
		}
		opts.Secure = true
		opts.TLSConfig = tlsCfg
	}

	for _, apply := range []func(*natsgo.Options) error{
		authOpt(cfg),
	} {
		if apply == nil {
			continue
		}
		if err := apply(&opts); err != nil {
			return nil, err
		}
	}

	if logger != nil {
		opts.ConnectedCB = func(nc *natsgo.Conn) { logger.Info("nats connected", "url", nc.ConnectedUrl()) }
		opts.ReconnectedCB = func(nc *natsgo.Conn) { logger.Info("nats reconnected", "url", nc.ConnectedUrl()) }
		opts.DisconnectedErrCB = func(_ *natsgo.Conn, err error) { logger.Warn("nats disconnected", "error", err) }
		opts.ClosedCB = func(nc *natsgo.Conn) { logger.Info("nats closed", "last_error", nc.LastError()) }
		opts.AsyncErrorCB = func(_ *natsgo.Conn, sub *natsgo.Subscription, err error) {
			subject := ""
			if sub != nil {
				subject = sub.Subject
			}
			logger.Warn("nats async error", "subject", subject, "error", err)
		}
		opts.ReconnectErrCB = func(_ *natsgo.Conn, err error) { logger.Warn("nats reconnect failed", "error", err) }
	}
	return opts.Connect()
}

func authOpt(cfg NATSClientConfig) func(*natsgo.Options) error {
	switch {
	case cfg.CredentialsFile != "" && cfg.CredentialsSeedFile != "":
		return natsgo.UserCredentials(cfg.CredentialsFile, cfg.CredentialsSeedFile)
	case cfg.CredentialsFile != "":
		return natsgo.UserCredentials(cfg.CredentialsFile)
	case cfg.UserJWT != "" && cfg.UserSeed != "":
		return natsgo.UserJWTAndSeed(cfg.UserJWT, cfg.UserSeed)
	case cfg.NKeySeedFile != "":
		return func(o *natsgo.Options) error {
			opt, err := natsgo.NkeyOptionFromSeed(cfg.NKeySeedFile)
			if err != nil {
				return err
			}
			return opt(o)
		}
	default:
		return nil
	}
}

func NATSURLs(cfg NATSClientConfig) string {
	if len(cfg.URLs) > 0 {
		return strings.Join(cfg.URLs, ",")
	}
	return cfg.URL
}
