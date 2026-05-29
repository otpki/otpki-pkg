package clientcfg

import (
	"context"
	"encoding"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	aws2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

var _ encoding.TextUnmarshaler = (*AWSRetryMode)(nil)
var _ encoding.TextUnmarshaler = (*AWSClientLogModeFlag)(nil)

type AWSRetryMode string

const (
	AWSRetryModeStandard AWSRetryMode = "standard"
	AWSRetryModeAdaptive AWSRetryMode = "adaptive"
	AWSRetryModeLegacy   AWSRetryMode = "legacy"
)

func (m *AWSRetryMode) UnmarshalText(text []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(text))) {
	case "", "standard":
		*m = AWSRetryModeStandard
	case "adaptive":
		*m = AWSRetryModeAdaptive
	case "legacy":
		*m = AWSRetryModeLegacy
	default:
		return fmt.Errorf("invalid aws retry mode %q", string(text))
	}
	return nil
}

func (m AWSRetryMode) awsValue() aws2.RetryMode {
	switch m {
	case AWSRetryModeAdaptive:
		return aws2.RetryModeAdaptive
	default:
		return aws2.RetryModeStandard
	}
}

type AWSClientLogModeFlag string

const (
	AWSLogSigning          AWSClientLogModeFlag = "signing"
	AWSLogRetries          AWSClientLogModeFlag = "retries"
	AWSLogRequest          AWSClientLogModeFlag = "request"
	AWSLogRequestWithBody  AWSClientLogModeFlag = "request_with_body"
	AWSLogResponse         AWSClientLogModeFlag = "response"
	AWSLogResponseWithBody AWSClientLogModeFlag = "response_with_body"
)

func (f *AWSClientLogModeFlag) UnmarshalText(text []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(text))) {
	case "signing":
		*f = AWSLogSigning
	case "retries":
		*f = AWSLogRetries
	case "request":
		*f = AWSLogRequest
	case "request_with_body", "request-with-body":
		*f = AWSLogRequestWithBody
	case "response":
		*f = AWSLogResponse
	case "response_with_body", "response-with-body":
		*f = AWSLogResponseWithBody
	default:
		return fmt.Errorf("invalid aws client log mode %q", string(text))
	}
	return nil
}

type AWSClientConfig struct {
	Region                 string                 `mapstructure:"region"`
	Profile                string                 `mapstructure:"profile"`
	SharedConfigFiles      []string               `mapstructure:"shared_config_files"`
	SharedCredentialsFiles []string               `mapstructure:"shared_credentials_files"`
	AccessKeyID            string                 `mapstructure:"access_key_id"`
	SecretAccessKey        string                 `mapstructure:"secret_access_key"`
	SessionToken           string                 `mapstructure:"session_token"`
	BaseEndpoint           string                 `mapstructure:"base_endpoint"`
	AppID                  string                 `mapstructure:"app_id"`
	RetryMaxAttempts       int                    `mapstructure:"retry_max_attempts"`
	RetryMode              AWSRetryMode           `mapstructure:"retry_mode"`
	ClientLogModes         []AWSClientLogModeFlag `mapstructure:"client_log_modes"`
	RequestTimeout         time.Duration          `mapstructure:"request_timeout"`
	TLS                    *TLSConfig             `mapstructure:"tls"`
	CustomCABundleFile     string                 `mapstructure:"custom_ca_bundle_file"`
	DisableSQSChecksum     bool                   `mapstructure:"disable_sqs_checksum_validation"`
}

func DefaultAWSClientConfig() AWSClientConfig { return AWSClientConfig{} }

func LoadAWSConfig(ctx context.Context, cfg AWSClientConfig, logger *slog.Logger) (aws2.Config, error) {
	var optFns []func(*config.LoadOptions) error
	if cfg.Region != "" {
		optFns = append(optFns, config.WithRegion(cfg.Region))
	}
	if cfg.Profile != "" {
		optFns = append(optFns, config.WithSharedConfigProfile(cfg.Profile))
	}
	if len(cfg.SharedConfigFiles) > 0 {
		optFns = append(optFns, config.WithSharedConfigFiles(cfg.SharedConfigFiles))
	}
	if len(cfg.SharedCredentialsFiles) > 0 {
		optFns = append(optFns, config.WithSharedCredentialsFiles(cfg.SharedCredentialsFiles))
	}
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" || cfg.SessionToken != "" {
		optFns = append(optFns, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)))
	}
	if cfg.RetryMaxAttempts > 0 {
		optFns = append(optFns, config.WithRetryMaxAttempts(cfg.RetryMaxAttempts))
	}
	if cfg.RetryMode != "" {
		optFns = append(optFns, config.WithRetryMode(cfg.RetryMode.awsValue()))
	}
	if logger != nil {
		optFns = append(optFns, config.WithLogger(NewSmithyLogger(logger)))
		mode := aws2.ClientLogMode(0)
		for _, flag := range cfg.ClientLogModes {
			switch flag {
			case AWSLogSigning:
				mode |= aws2.LogSigning
			case AWSLogRetries:
				mode |= aws2.LogRetries
			case AWSLogRequest:
				mode |= aws2.LogRequest
			case AWSLogRequestWithBody:
				mode |= aws2.LogRequestWithBody
			case AWSLogResponse:
				mode |= aws2.LogResponse
			case AWSLogResponseWithBody:
				mode |= aws2.LogResponseWithBody
			}
		}
		if mode != 0 {
			optFns = append(optFns, config.WithClientLogMode(mode))
		}
	}
	if cfg.CustomCABundleFile != "" {
		f, err := os.Open(cfg.CustomCABundleFile)
		if err != nil {
			return aws2.Config{}, err
		}
		defer f.Close()
		optFns = append(optFns, config.WithCustomCABundle(f))
	}
	if cfg.RequestTimeout > 0 || cfg.TLS != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.TLS != nil {
			tlsCfg, err := BuildTLSConfig(cfg.TLS)
			if err != nil {
				return aws2.Config{}, err
			}
			transport.TLSClientConfig = tlsCfg
		}
		client := &http.Client{Transport: transport, Timeout: cfg.RequestTimeout}
		optFns = append(optFns, config.WithHTTPClient(client))
	}
	return config.LoadDefaultConfig(ctx, optFns...)
}

func NewSNSClient(awsCfg aws2.Config, cfg AWSClientConfig, logger *slog.Logger) *sns.Client {
	return sns.NewFromConfig(awsCfg, func(o *sns.Options) {
		if cfg.BaseEndpoint != "" {
			o.BaseEndpoint = &cfg.BaseEndpoint
		}
		if logger != nil {
			o.Logger = NewSmithyLogger(logger)
		}
	})
}

func NewSQSClient(awsCfg aws2.Config, cfg AWSClientConfig, logger *slog.Logger) *sqs.Client {
	return sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.BaseEndpoint != "" {
			o.BaseEndpoint = &cfg.BaseEndpoint
		}
		if cfg.DisableSQSChecksum {
			o.DisableMessageChecksumValidation = true
		}
		if logger != nil {
			o.Logger = NewSmithyLogger(logger)
		}
	})
}
