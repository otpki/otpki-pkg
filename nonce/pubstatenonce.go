package nonce

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/otpki/otpki-pkg/config"
	spfystate "github.com/otpki/otpki-pkg/spfy/anchor"
)

type PubStateNonceManager struct {
	config config.Config
	store  spfystate.Store[[]byte]
	ttl    time.Duration
}

type pubStateNonceRecord struct {
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
	Consumed  bool      `json:"consumed"`
}

func NewPubStateNonceManager(conf config.Config, store spfystate.Store[[]byte]) *PubStateNonceManager {
	return NewPubStateNonceManagerWithTTL(conf, store, expiration)
}

func NewPubStateNonceManagerWithTTL(conf config.Config, store spfystate.Store[[]byte], ttl time.Duration) *PubStateNonceManager {
	return &PubStateNonceManager{
		config: conf,
		store:  store,
		ttl:    ttl,
	}
}

func (m *PubStateNonceManager) CreateNonce(ctx context.Context) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	nonce := base64.RawURLEncoding.EncodeToString(bytes)
	return m.AddNonce(ctx, nonce)
}

func (m *PubStateNonceManager) AddNonce(ctx context.Context, nonce string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	encoded, err := m.encodeRecord(m.newRecord(nonce, false))
	if err != nil {
		return "", err
	}

	if _, err := m.store.Put(ctx, m.key(nonce), encoded, spfystate.WriteOptions{TTL: m.ttl}); err != nil {
		return "", err
	}

	return nonce, nil
}

func (m *PubStateNonceManager) CheckNonce(ctx context.Context, nonce string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	for range maxOptimisticRetries {
		resp, err := m.store.Get(ctx, m.key(nonce))
		if err != nil {
			if errors.Is(err, spfystate.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		if len(resp.Value) == 0 {
			return false, nil
		}

		record, err := m.decodeRecord(resp.Value)
		if err != nil {
			return false, err
		}
		if record.expired() || record.Consumed {
			return false, nil
		}

		if record.ExpiresAt.IsZero() {
			record.ExpiresAt = time.Now().Add(m.ttl)
		}
		record.Consumed = true
		encoded, err := m.encodeRecord(record)
		if err != nil {
			return false, err
		}

		remaining := time.Until(record.ExpiresAt)
		if remaining <= 0 {
			return false, nil
		}
		_, err = m.store.CompareAndSet(ctx, m.key(nonce), resp.ETag, encoded, spfystate.WriteOptions{TTL: remaining})
		if err == nil {
			return true, nil
		}
		if isETagMismatchErr(err) {
			continue
		}
		return false, err
	}

	return false, nil
}

func (m *PubStateNonceManager) AddUniqueNonce(ctx context.Context, nonce string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	for range maxOptimisticRetries {
		record := m.newRecord(nonce, false)
		encoded, err := m.encodeRecord(record)
		if err != nil {
			return false, err
		}

		_, err = m.store.Create(ctx, m.key(nonce), encoded, spfystate.WriteOptions{TTL: m.ttl})
		if err == nil {
			return true, nil
		}

		if !errors.Is(err, spfystate.ErrAlreadyExists) {
			return false, err
		}

		resp, err := m.store.Get(ctx, m.key(nonce))
		if err != nil {
			if errors.Is(err, spfystate.ErrNotFound) {
				continue
			}
			return false, err
		}
		if len(resp.Value) == 0 {
			continue
		}

		current, err := m.decodeRecord(resp.Value)
		if err != nil {
			return false, err
		}
		if !current.expired() && !current.Consumed {
			return false, nil
		}

		_, err = m.store.CompareAndSet(ctx, m.key(nonce), resp.ETag, encoded, spfystate.WriteOptions{TTL: m.ttl})
		if err == nil {
			return true, nil
		}
		if isETagMismatchErr(err) {
			continue
		}
		return false, err
	}

	return false, nil
}

func (m *PubStateNonceManager) newRecord(nonce string, consumed bool) pubStateNonceRecord {
	return pubStateNonceRecord{
		Nonce:     nonce,
		ExpiresAt: time.Now().Add(m.ttl),
		Consumed:  consumed,
	}
}

func (m *PubStateNonceManager) key(nonce string) string {
	return m.config.App.Keyspace.Key("nonces", nonce)
}

func (m *PubStateNonceManager) encodeRecord(record pubStateNonceRecord) ([]byte, error) {
	return json.Marshal(record)
}

func (m *PubStateNonceManager) decodeRecord(data []byte) (pubStateNonceRecord, error) {
	var record pubStateNonceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return pubStateNonceRecord{}, err
	}
	return record, nil
}

func (r pubStateNonceRecord) expired() bool {
	return !r.ExpiresAt.IsZero() && time.Now().After(r.ExpiresAt)
}

func isETagMismatchErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, spfystate.ErrETagMismatch) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "wrong last sequence") || strings.Contains(msg, "etag mismatch")
}

var _ NonceManager = (*PubStateNonceManager)(nil)
