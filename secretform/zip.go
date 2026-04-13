package secretform

import (
	"archive/zip"
	"bytes"
	"fmt"

	"github.com/samber/oops"
)

// GenerateZip generates a ZIP archive (as bytes) containing one PDF per secret.
// Each PDF is named: SecretForm_<kcv>.pdf
//
// This is designed for use in HTTP responses (Content-Type: application/zip).
func GenerateZip(secrets []string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	seen := map[string]int{} // handle rare name collisions safely

	for i, secret := range secrets {
		res, err := Generate(secret)
		if err != nil {
			_ = zw.Close()
			return nil, oops.Errorf("secret %d: %w", i, err)
		}

		baseName := fmt.Sprintf("SecretForm_%s.pdf", res.KCV)
		name := baseName
		if n := seen[baseName]; n > 0 {
			// extremely unlikely collision, but deterministic handling
			name = fmt.Sprintf("SecretForm_%s_%d.pdf", res.KCV, n+1)
		}
		seen[baseName]++

		w, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := w.Write(res.PDF); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
