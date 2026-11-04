package helpers

import (
	"context"

	"github.com/otpki/otpki-pkg/oopsie"
	"github.com/otpki/otpki-pkg/shamir"
)

func ShamirHexStringsToSecret(ctx context.Context, hexStrings []string) (string, error) {
	shares := make([][]byte, len(hexStrings))
	var err error
	for i, share := range hexStrings {
		shares[i], err = ConvertHexToBytes(share)
		if err != nil {
			return "", oopsie.In(ServiceName).Wrapf(ctx, err, "decoding Shamir share %d", i)
		}
	}
	secret, err := shamir.Combine(shares)
	if err != nil {
		return "", oopsie.In(ServiceName).Wrapf(ctx, err, "combining Shamir shares")
	}
	return string(secret), nil
}

func SecretToShamirHexStrings(ctx context.Context, secret string, numShares, threshold int64) ([]string, error) {
	shares, err := shamir.Split([]byte(secret), int(numShares), int(threshold))
	if err != nil {
		return nil, oopsie.In(ServiceName).Wrapf(ctx, err, "splitting secret into Shamir shares")
	}
	hexShares := make([]string, len(shares))
	for i, share := range shares {
		hexShares[i] = ConvertBytesToHex(share)
	}
	return hexShares, nil
}
