package clientcfg

import (
	"crypto/tls"
	"crypto/x509"
	"encoding"
	"fmt"
	"os"
	"strings"
)

var _ encoding.TextUnmarshaler = (*TLSVersion)(nil)

type TLSVersion uint16

const (
	TLSVersion10 TLSVersion = tls.VersionTLS10
	TLSVersion11 TLSVersion = tls.VersionTLS11
	TLSVersion12 TLSVersion = tls.VersionTLS12
	TLSVersion13 TLSVersion = tls.VersionTLS13
)

func (v TLSVersion) String() string {
	switch v {
	case TLSVersion10:
		return "1.0"
	case TLSVersion11:
		return "1.1"
	case TLSVersion12:
		return "1.2"
	case TLSVersion13:
		return "1.3"
	default:
		return fmt.Sprintf("TLSVersion(%d)", uint16(v))
	}
}

func (v *TLSVersion) UnmarshalText(text []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(text))) {
	case "", "1.2", "tls1.2", "tls12":
		*v = TLSVersion12
		return nil
	case "1.0", "tls1.0", "tls10":
		*v = TLSVersion10
		return nil
	case "1.1", "tls1.1", "tls11":
		*v = TLSVersion11
		return nil
	case "1.3", "tls1.3", "tls13":
		*v = TLSVersion13
		return nil
	default:
		return fmt.Errorf("invalid TLS version %q", string(text))
	}
}

type TLSConfig struct {
	Enabled            bool       `mapstructure:"enabled"`
	ServerName         string     `mapstructure:"server_name"`
	InsecureSkipVerify bool       `mapstructure:"insecure_skip_verify"`
	MinVersion         TLSVersion `mapstructure:"min_version"`
	MaxVersion         TLSVersion `mapstructure:"max_version"`
	CAFile             string     `mapstructure:"ca_file"`
	CertFile           string     `mapstructure:"cert_file"`
	KeyFile            string     `mapstructure:"key_file"`
}

func BuildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	if cfg == nil {
		return nil, nil
	}
	if !cfg.Enabled && cfg.ServerName == "" && !cfg.InsecureSkipVerify && cfg.MinVersion == 0 && cfg.MaxVersion == 0 && cfg.CAFile == "" && cfg.CertFile == "" && cfg.KeyFile == "" {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}
	if cfg.MinVersion != 0 {
		tlsCfg.MinVersion = uint16(cfg.MinVersion)
	}
	if cfg.MaxVersion != 0 {
		tlsCfg.MaxVersion = uint16(cfg.MaxVersion)
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("failed appending CA bundle %q", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, fmt.Errorf("both cert_file and key_file are required for TLS client auth")
		}
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return tlsCfg, nil
}
