package helpers

import (
	"crypto"
	"crypto/ed25519"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strings"

	"github.com/samber/oops"
	"go.step.sm/crypto/pemutil"
	"golang.org/x/crypto/ssh"
)

func MarshalPrivateKeyDER(privKey crypto.PrivateKey) ([]byte, error) {
	return x509.MarshalPKCS8PrivateKey(privKey)
}

func MarshalPrivateKey(privKey crypto.PrivateKey) ([]byte, error) {
	privateBytes, err := MarshalPrivateKeyDER(privKey)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "marshalling private key")
	}

	privatePEM := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateBytes,
	}

	return pem.EncodeToMemory(privatePEM), nil
}

func UnmarshalPrivateKey(privatePEM []byte) (crypto.PrivateKey, error) {
	return UnmarshalPrivateKeyWithPassword(privatePEM, "")
}

func UnmarshalPrivateKeyWithPassword(privateKeyBytes []byte, password string) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(privateKeyBytes)
	if block == nil {
		return nil, oops.In(ServiceName).New("failed to decode PEM block or block type is not 'PRIVATE KEY'")
	}
	if !strings.HasSuffix(block.Type, "PRIVATE KEY") {
		return nil, oops.In(ServiceName).New("failed to decode PEM block or block type is not 'PRIVATE KEY'")
	}

	if block.Type == "ENCRYPTED PRIVATE KEY" {
		if password == "" {
			return nil, oops.In(ServiceName).New("password required for encrypted private key")
		}

		decryptedPKCS8, err := pemutil.DecryptPKCS8PrivateKey(block.Bytes, []byte(password))
		if err != nil {
			return nil, oops.In(ServiceName).Wrapf(err, "decrypting PKCS8 private key")
		}

		return parsePEMPrivateKeyBlock("PRIVATE KEY", decryptedPKCS8)
	}

	if block.Type == "OPENSSH PRIVATE KEY" {
		return parseOpenSSHPrivateKey(privateKeyBytes, password)
	}

	if x509.IsEncryptedPEMBlock(block) {
		if password == "" {
			return nil, oops.In(ServiceName).New("password required for encrypted private key")
		}

		decryptedDER, err := x509.DecryptPEMBlock(block, []byte(password))
		if err != nil {
			return nil, oops.In(ServiceName).Wrapf(err, "decrypting PEM private key")
		}

		return parsePEMPrivateKeyBlock(block.Type, decryptedDER)
	}

	return parsePEMPrivateKeyBlock(block.Type, block.Bytes)
}

func ParseAndNormalizePrivateKeyToPKCS8PEM(privateKeyBytes []byte, password string) (crypto.PrivateKey, []byte, error) {
	privateKey, err := UnmarshalPrivateKeyWithPassword(privateKeyBytes, password)
	if err != nil {
		return nil, nil, oops.In(ServiceName).Wrapf(err, "parsing private key")
	}

	normalized, err := MarshalPrivateKey(privateKey)
	if err != nil {
		return nil, nil, oops.In(ServiceName).Wrapf(err, "marshalling private key")
	}

	return privateKey, normalized, nil
}

func parsePEMPrivateKeyBlock(blockType string, der []byte) (crypto.PrivateKey, error) {
	privateKey, err := UnmarshalPrivateKeyDER(der)
	if err == nil {
		return privateKey, nil
	}

	switch blockType {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(der)
		if err != nil {
			return nil, oops.In(ServiceName).Wrapf(err, "parsing RSA private key as PKCS8 or PKCS1")
		}
		return privateKey, nil
	case "EC PRIVATE KEY":
		privateKey, err = x509.ParseECPrivateKey(der)
		if err != nil {
			return nil, oops.In(ServiceName).Wrapf(err, "parsing EC private key as PKCS8 or SEC1")
		}
		return privateKey, nil
	default:
		return nil, oops.In(ServiceName).Wrapf(err, "parsing private key as PKCS8")
	}
}

func parseOpenSSHPrivateKey(privateKeyBytes []byte, password string) (crypto.PrivateKey, error) {
	var (
		privateKey any
		err        error
	)

	if password != "" {
		privateKey, err = ssh.ParseRawPrivateKeyWithPassphrase(privateKeyBytes, []byte(password))
	} else {
		privateKey, err = ssh.ParseRawPrivateKey(privateKeyBytes)
	}
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "parsing OpenSSH private key")
	}

	if ed25519PrivateKey, ok := privateKey.(*ed25519.PrivateKey); ok {
		return *ed25519PrivateKey, nil
	}

	return privateKey, nil
}

func UnmarshalPrivateKeyDER(der []byte) (crypto.PrivateKey, error) {
	return x509.ParsePKCS8PrivateKey(der)
}

func MarshalPublicKeyDER(pubKey crypto.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pubKey)
}

func MarshalPublicKey(pubKey crypto.PublicKey) ([]byte, error) {
	publicBytes, err := MarshalPublicKeyDER(pubKey)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "marshalling public key to PKIX")
	}

	publicPEM := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicBytes,
	}

	return pem.EncodeToMemory(publicPEM), nil
}

func UnmarshalPublicKey(publicPEM []byte) (crypto.PublicKey, error) {
	block, _ := pem.Decode(publicPEM)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, oops.In(ServiceName).New("failed to decode PEM block or block type is not 'PUBLIC KEY'")
	}
	return UnmarshalPublicKeyDER(block.Bytes)
}

func UnmarshalPublicKeyDER(der []byte) (crypto.PublicKey, error) {
	return x509.ParsePKIXPublicKey(der)
}

func GetKeySHA1Hash(pubKey crypto.PublicKey) (string, error) {
	rawPubKeyBitStr, err := GetRawPublicKeyBitStr(pubKey)
	if err != nil {
		return "", oops.In(ServiceName).Wrapf(err, "getting raw public key bits for hashing")
	}
	hasher := sha1.New()
	hasher.Write(rawPubKeyBitStr.RightAlign())
	publicKeyHashSHA1Bytes := hasher.Sum(nil)
	return hex.EncodeToString(publicKeyHashSHA1Bytes), nil
}
