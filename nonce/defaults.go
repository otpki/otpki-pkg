package nonce

import "time"

var (
	expiration = 15 * time.Minute
	opTimeout  = 10 * time.Second
)

const maxOptimisticRetries = 4
