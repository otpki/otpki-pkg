package nonce

import "context"

type NonceManager interface {
	CreateNonce(ctx context.Context) (string, error)
	AddNonce(ctx context.Context, nonce string) (string, error)
	CheckNonce(ctx context.Context, nonce string) (bool, error)
	AddUniqueNonce(ctx context.Context, nonce string) (bool, error)
}
