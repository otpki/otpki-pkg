package helpers

import (
	"connectrpc.com/connect"
	"github.com/hashicorp/vault/shamir"
)

func ShamirHexStringsToSecret(hexStrings []string) (string, error) {
	shares := make([][]byte, len(hexStrings))
	var err error
	for i, share := range hexStrings {
		shares[i], err = ConvertHexToBytes(share)
		if err != nil {
			return "", err
		}
	}
	secret, err := shamir.Combine(shares)
	if err != nil {
		return "", nil
	}
	return string(secret), nil
}

func SecretToShamirHexStrings(secret string, numShares, threshold int64) ([]string, error) {
	shares, err := shamir.Split([]byte(secret), int(numShares), int(threshold))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	hexShares := make([]string, len(shares))
	for i, share := range shares {
		hexShares[i] = ConvertBytesToHex(share)
	}
	return hexShares, nil
}
