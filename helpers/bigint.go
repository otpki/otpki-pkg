package helpers

import (
	"math/big"

	"github.com/samber/oops"
)

func ParseBigInt(v string) (*big.Int, error) {
	if value, ok := big.NewInt(0).SetString(v, 10); ok {
		return value, nil
	}
	return nil, oops.In(ServiceName).Errorf("failed to parse %s to bigint", v)
}

func AddBigInt(a *big.Int, b *big.Int) *big.Int {
	return big.NewInt(0).Add(a, b)
}
