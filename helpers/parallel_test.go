package helpers_test

import (
	"testing"

	"github.com/otpki/otpki-pkg/helpers"
	"github.com/stretchr/testify/assert"
)

func TestParallelRunner(t *testing.T) {
	data := []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
	results, err := helpers.ParallelEach(func(i int) ([]int, error) {
		return []int{2 * i}, nil
	}, data)
	assert.Nil(t, err)
	assert.Equal(t, len(data), len(results))
	// Results are unordered, but each should be even
	for _, v := range results {
		assert.True(t, v%2 == 0)
	}
}
