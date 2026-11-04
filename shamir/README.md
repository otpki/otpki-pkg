# Shamir secret sharing

This package is extracted from `github.com/hashicorp/vault/shamir` at
commit `0e7c41b93621` (module version
`v1.2.1-0.20261022202749-0e7c41b93621`). Source, upstream tests, and the
package-specific MPL-2.0 license are retained here. The root Vault server module
is not required for these operations.

Shares retain Vault's byte format: one GF(256) polynomial value per secret byte,
followed by the nonzero x-coordinate. Existing hex-encoded shares must remain
readable, and new shares must remain readable by the prior Vault implementation.

The obsolete `math/rand.Seed` call was removed: it is a no-op by default on the
supported Go 1.27 toolchain. Public x-coordinates remain a permutation of 1..255; secret
polynomial coefficients continue to come from `crypto/rand`. Cryptographic field
arithmetic and interpolation are retained from upstream. Local naming and loop
style follow the repository lint rules; narrow security-check annotations
document byte ranges and the non-secret coordinate permutation.

When importing upstream fixes, review the source changes and rerun upstream
unit tests, lint, and vulnerability scanning. This code
retains its MPL-2.0 license; see [LICENSE](LICENSE).
