package nonce

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/otpki/otpki-pkg/log"
	valkey "github.com/redis/go-redis/v9"
)

// Redis/Valkey backed nonce manager

var (
	expiration = time.Minute * 15
	opTimeout  = time.Second * 10
)

type VKNonceManager struct {
	client valkey.UniversalClient
	logger log.Logger
	prefix string
}

func NewVKNonceManager(client valkey.UniversalClient, keyPrefix string, logger log.Logger) (*VKNonceManager, error) {
	return &VKNonceManager{
		client: client,
		logger: logger,
		prefix: keyPrefix + ":nonces:",
	}, nil
}

func (v *VKNonceManager) CreateNonce(ctx context.Context) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	nonce := base64.RawURLEncoding.EncodeToString(bytes)
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	// Store in the map
	out := v.client.SetEx(ctx, v.prefix+nonce, nonce, expiration)

	return nonce, out.Err()
}

func (v *VKNonceManager) AddNonce(ctx context.Context, nonce string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	// Store in the map
	out := v.client.SetEx(ctx, v.prefix+nonce, nonce, expiration)
	return nonce, out.Err()
}

// Returns true if source contains nonce and then remove it, else return false
func (v *VKNonceManager) CheckNonce(ctx context.Context, nonce string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	out := v.client.GetDel(ctx, v.prefix+nonce)
	if out.Err() != nil {
		v.logger.Err(out.Err()).Msgf("error on lookup of %s", v.prefix+nonce)
		return false, nil
	}
	return out.Val() != "", nil
}

func (v *VKNonceManager) AddUniqueNonce(ctx context.Context, nonce string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := v.client.SetArgs(ctx, v.prefix+nonce, nonce, valkey.SetArgs{
		Mode: string(valkey.NX),
		TTL:  expiration,
	}).Result()
	if err != nil {
		return false, err
	}
	return true, nil
}
