package natsjetstream

import (
	"encoding"
	"strings"
	"time"

	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
	"github.com/samber/oops"
)

type NATSJetStreamStorage string

const (
	NATSJetStreamFileStorage   NATSJetStreamStorage = "file"
	NATSJetStreamMemoryStorage NATSJetStreamStorage = "memory"
)

var _ encoding.TextUnmarshaler = (*NATSJetStreamStorage)(nil)

func (s *NATSJetStreamStorage) UnmarshalText(text []byte) error {
	v := strings.ToLower(strings.TrimSpace(string(text)))
	switch v {
	case "", "file", "filestorage", "file_storage":
		*s = NATSJetStreamFileStorage
		return nil
	case "memory", "mem", "memorystorage", "memory_storage":
		*s = NATSJetStreamMemoryStorage
		return nil
	default:
		return oops.Errorf("unknown NATS JetStream storage %q", string(text))
	}
}

func (s NATSJetStreamStorage) MarshalText() ([]byte, error) {
	if s == "" {
		return []byte(NATSJetStreamFileStorage), nil
	}
	return []byte(s), nil
}

type Config struct {
	StreamName             string               `mapstructure:"stream_name"`
	SubjectPrefix          string               `mapstructure:"subject_prefix"`
	Storage                NATSJetStreamStorage `mapstructure:"storage"`
	OperationTimeout       time.Duration        `mapstructure:"operation_timeout"`
	SubjectDeleteMarkerTTL time.Duration        `mapstructure:"subject_delete_marker_ttl"`
	WatchBuffer            int                  `mapstructure:"watch_buffer"`
}

func DefaultConfig() Config {
	return Config{
		StreamName:             "STATESTORE",
		Storage:                NATSJetStreamFileStorage,
		OperationTimeout:       internal.DefaultNATSOperationTimeout,
		SubjectDeleteMarkerTTL: time.Hour,
		WatchBuffer:            internal.DefaultWatchBuffer,
	}
}
