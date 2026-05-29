package internal

import "time"

const (
	DefaultWatchBuffer          = 128
	DefaultNATSOperationTimeout = 5 * time.Second
)

func NormalizeWatchBuffer(size int) int {
	if size <= 0 {
		return DefaultWatchBuffer
	}
	return size
}
