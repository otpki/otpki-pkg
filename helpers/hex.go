package helpers

import (
	"encoding/hex"
	"strings"
)

func ConvertHexToBytes(s string) ([]byte, error) {
	// lowercase all characters
	// remove all whitespace
	// trim any hex prefix (0x)
	normalizedHexString := strings.TrimPrefix(strings.ReplaceAll(strings.ToLower(s), " ", ""), "0x")
	return hex.DecodeString(normalizedHexString)
}

func ConvertBytesToHex(b []byte) string {
	return hex.EncodeToString(b)
}
