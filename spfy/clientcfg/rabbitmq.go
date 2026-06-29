package clientcfg

import (
	"crypto/tls"
	"encoding"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/samber/oops"
)

var _ encoding.TextUnmarshaler = (*RabbitMQSASLMechanism)(nil)

type RabbitMQSASLMechanism string

const (
	RabbitMQSASLPlain    RabbitMQSASLMechanism = "plain"
	RabbitMQSASLAMQPlain RabbitMQSASLMechanism = "amqplain"
	RabbitMQSASLExternal RabbitMQSASLMechanism = "external"
)

func (m *RabbitMQSASLMechanism) UnmarshalText(text []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(text))) {
	case "", "plain":
		*m = RabbitMQSASLPlain
	case "amqplain", "amq_plain":
		*m = RabbitMQSASLAMQPlain
	case "external":
		*m = RabbitMQSASLExternal
	default:
		return oops.Errorf("invalid rabbitmq sasl mechanism %q", string(text))
	}
	return nil
}

type RabbitMQSASLConfig struct {
	Mechanism RabbitMQSASLMechanism `mapstructure:"mechanism"`
	Username  string                `mapstructure:"username"`
	Password  string                `mapstructure:"password"`
}

type RabbitMQClientConfig struct {
	URL               string              `mapstructure:"url"`
	Host              string              `mapstructure:"host"`
	Port              int                 `mapstructure:"port"`
	Username          string              `mapstructure:"username"`
	Password          string              `mapstructure:"password"`
	Vhost             string              `mapstructure:"vhost"`
	ChannelMax        uint16              `mapstructure:"channel_max"`
	FrameSize         int                 `mapstructure:"frame_size"`
	Heartbeat         time.Duration       `mapstructure:"heartbeat"`
	Locale            string              `mapstructure:"locale"`
	ConnectionTimeout time.Duration       `mapstructure:"connection_timeout"`
	TLS               *TLSConfig          `mapstructure:"tls"`
	SASL              *RabbitMQSASLConfig `mapstructure:"sasl"`
	Properties        map[string]string   `mapstructure:"properties"`
}

func DefaultRabbitMQClientConfig() RabbitMQClientConfig {
	return RabbitMQClientConfig{
		URL:       "amqp://guest:guest@localhost:5672/",
		Host:      "localhost",
		Port:      5672,
		Username:  "guest",
		Password:  "guest",
		Vhost:     "/",
		Locale:    "en_US",
		Heartbeat: 10 * time.Second,
	}
}

func NewRabbitMQConnection(cfg RabbitMQClientConfig) (*amqp.Connection, error) {
	defaults := DefaultRabbitMQClientConfig()
	if cfg.URL == "" {
		if cfg.Host == "" {
			cfg.Host = defaults.Host
		}
		if cfg.Port == 0 {
			cfg.Port = defaults.Port
		}
		if cfg.Vhost == "" {
			cfg.Vhost = defaults.Vhost
		}
		if cfg.Username == "" && cfg.SASL == nil {
			cfg.Username = defaults.Username
		}
		if cfg.Password == "" && cfg.SASL == nil {
			cfg.Password = defaults.Password
		}
	}
	if cfg.Heartbeat == 0 {
		cfg.Heartbeat = defaults.Heartbeat
	}
	if cfg.Locale == "" {
		cfg.Locale = defaults.Locale
	}
	var tlsCfg *tls.Config
	if cfg.TLS != nil {
		var err error
		tlsCfg, err = BuildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, err
		}
	}
	amqpCfg := amqp.Config{ChannelMax: cfg.ChannelMax, FrameSize: cfg.FrameSize, Heartbeat: cfg.Heartbeat, Locale: cfg.Locale, TLSClientConfig: tlsCfg, Properties: amqp.Table{}}
	for k, v := range cfg.Properties {
		amqpCfg.Properties[k] = v
	}
	if cfg.ConnectionTimeout > 0 {
		dialer := &net.Dialer{Timeout: cfg.ConnectionTimeout}
		amqpCfg.Dial = func(network, addr string) (net.Conn, error) { return dialer.Dial(network, addr) }
	}
	if cfg.SASL != nil {
		switch cfg.SASL.Mechanism {
		case RabbitMQSASLAMQPlain:
			amqpCfg.SASL = []amqp.Authentication{&amqp.AMQPlainAuth{Username: cfg.SASL.Username, Password: cfg.SASL.Password}}
		case RabbitMQSASLExternal:
			amqpCfg.SASL = []amqp.Authentication{&amqp.ExternalAuth{}}
		default:
			amqpCfg.SASL = []amqp.Authentication{&amqp.PlainAuth{Username: cfg.SASL.Username, Password: cfg.SASL.Password}}
		}
	}
	return amqp.DialConfig(rabbitMQURL(cfg, tlsCfg != nil), amqpCfg)
}

func rabbitMQURL(cfg RabbitMQClientConfig, secure bool) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	scheme := "amqp"
	if secure {
		scheme = "amqps"
	}
	u := &url.URL{Scheme: scheme, Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), Path: cfg.Vhost}
	if cfg.Username != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.Username, cfg.Password)
	}
	return u.String()
}
