package clientcfg

import (
	"encoding"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pulsarclient "github.com/apache/pulsar-client-go/pulsar"
	pulsarlog "github.com/apache/pulsar-client-go/pulsar/log"
)

var _ encoding.TextUnmarshaler = (*PulsarAuthType)(nil)

type PulsarAuthType string

const (
	PulsarAuthTypeToken     PulsarAuthType = "token"
	PulsarAuthTypeTokenFile PulsarAuthType = "token_file"
	PulsarAuthTypeTLS       PulsarAuthType = "tls"
	PulsarAuthTypeBasic     PulsarAuthType = "basic"
	PulsarAuthTypeOAuth2    PulsarAuthType = "oauth2"
	PulsarAuthTypePlugin    PulsarAuthType = "plugin"
)

func (t *PulsarAuthType) UnmarshalText(text []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(text))) {
	case "", "token":
		*t = PulsarAuthTypeToken
	case "token_file", "tokenfile":
		*t = PulsarAuthTypeTokenFile
	case "tls":
		*t = PulsarAuthTypeTLS
	case "basic":
		*t = PulsarAuthTypeBasic
	case "oauth2":
		*t = PulsarAuthTypeOAuth2
	case "plugin":
		*t = PulsarAuthTypePlugin
	default:
		return fmt.Errorf("invalid pulsar auth type %q", string(text))
	}
	return nil
}

type PulsarAuthConfig struct {
	Type         PulsarAuthType    `mapstructure:"type"`
	Token        string            `mapstructure:"token"`
	TokenFile    string            `mapstructure:"token_file"`
	Username     string            `mapstructure:"username"`
	Password     string            `mapstructure:"password"`
	Certificate  string            `mapstructure:"certificate"`
	PrivateKey   string            `mapstructure:"private_key"`
	PluginName   string            `mapstructure:"plugin_name"`
	PluginParams string            `mapstructure:"plugin_params"`
	Params       map[string]string `mapstructure:"params"`
}

type PulsarClientConfig struct {
	URL                     string            `mapstructure:"url"`
	ConnectionTimeout       time.Duration     `mapstructure:"connection_timeout"`
	OperationTimeout        time.Duration     `mapstructure:"operation_timeout"`
	KeepAliveInterval       time.Duration     `mapstructure:"keep_alive_interval"`
	Authentication          *PulsarAuthConfig `mapstructure:"authentication"`
	TLS                     *TLSConfig        `mapstructure:"tls"`
	TLSAllowInsecure        bool              `mapstructure:"tls_allow_insecure"`
	TLSValidateHostname     bool              `mapstructure:"tls_validate_hostname"`
	ListenerName            string            `mapstructure:"listener_name"`
	MaxConnectionsPerBroker int               `mapstructure:"max_connections_per_broker"`
	ConnectionMaxIdleTime   time.Duration     `mapstructure:"connection_max_idle_time"`
	MemoryLimitBytes        int64             `mapstructure:"memory_limit_bytes"`
	Description             string            `mapstructure:"description"`
	LookupProperties        map[string]string `mapstructure:"lookup_properties"`
}

func DefaultPulsarClientConfig() PulsarClientConfig {
	return PulsarClientConfig{
		URL:                     "pulsar://localhost:6650",
		ConnectionTimeout:       5 * time.Second,
		OperationTimeout:        30 * time.Second,
		KeepAliveInterval:       30 * time.Second,
		MaxConnectionsPerBroker: 1,
	}
}

func NewPulsarClient(cfg PulsarClientConfig, logger *slog.Logger) (pulsarclient.Client, error) {
	defaults := DefaultPulsarClientConfig()
	if cfg.URL == "" {
		cfg.URL = defaults.URL
	}
	opts := pulsarclient.ClientOptions{
		URL:                        cfg.URL,
		ConnectionTimeout:          cfg.ConnectionTimeout,
		OperationTimeout:           cfg.OperationTimeout,
		KeepAliveInterval:          cfg.KeepAliveInterval,
		TLSAllowInsecureConnection: cfg.TLSAllowInsecure,
		TLSValidateHostname:        cfg.TLSValidateHostname,
		ListenerName:               cfg.ListenerName,
		MaxConnectionsPerBroker:    cfg.MaxConnectionsPerBroker,
		ConnectionMaxIdleTime:      cfg.ConnectionMaxIdleTime,
		MemoryLimitBytes:           cfg.MemoryLimitBytes,
		Description:                cfg.Description,
		LookupProperties:           cfg.LookupProperties,
	}
	if logger != nil {
		opts.Logger = pulsarlog.NewLoggerWithSlog(logger)
	}
	if cfg.Authentication != nil {
		auth, err := buildPulsarAuth(*cfg.Authentication)
		if err != nil {
			return nil, err
		}
		opts.Authentication = auth
	}
	if cfg.TLS != nil {
		tlsCfg, err := BuildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, err
		}
		opts.TLSConfig = tlsCfg
		opts.TLSCertificateFile = cfg.TLS.CertFile
		opts.TLSKeyFilePath = cfg.TLS.KeyFile
		opts.TLSTrustCertsFilePath = cfg.TLS.CAFile
		if cfg.TLS.MinVersion != 0 {
			opts.TLSMinVersion = uint16(cfg.TLS.MinVersion)
		}
		if cfg.TLS.MaxVersion != 0 {
			opts.TLSMaxVersion = uint16(cfg.TLS.MaxVersion)
		}
	}
	return pulsarclient.NewClient(opts)
}

func buildPulsarAuth(cfg PulsarAuthConfig) (pulsarclient.Authentication, error) {
	switch cfg.Type {
	case PulsarAuthTypeToken, "":
		return pulsarclient.NewAuthenticationToken(cfg.Token), nil
	case PulsarAuthTypeTokenFile:
		return pulsarclient.NewAuthenticationTokenFromFile(cfg.TokenFile), nil
	case PulsarAuthTypeTLS:
		return pulsarclient.NewAuthenticationTLS(cfg.Certificate, cfg.PrivateKey), nil
	case PulsarAuthTypeBasic:
		return pulsarclient.NewAuthenticationBasic(cfg.Username, cfg.Password)
	case PulsarAuthTypeOAuth2:
		return pulsarclient.NewAuthenticationOAuth2(cfg.Params), nil
	case PulsarAuthTypePlugin:
		return pulsarclient.NewAuthentication(cfg.PluginName, cfg.PluginParams)
	default:
		return nil, fmt.Errorf("unsupported pulsar auth type %q", cfg.Type)
	}
}
