# Third-party notices

The root [Apache License 2.0](LICENSE) does not replace the notices or terms for
third-party code and assets. Preserve those notices with distributions.

## DNSSEC validator

Code adapted from `github.com/folbricht/routedns/dnssec` at commit
`2d34b9565853910155a89c499556175a72b1f47b` retains the BSD 3-Clause license in
[dnssec/LICENSE](dnssec/LICENSE). Compatibility helpers adapted from miekg/dns
retain the BSD 3-Clause notice in
[dnssec/LICENSE-miekg-dns](dnssec/LICENSE-miekg-dns). Source-file notices and the
[package guide](dnssec/README.md) describe the provenance.

## Logging adapters

The logging code identifies adaptations from `github.com/rs/zerolog`. Its MIT
license is retained in [LICENSES/zerolog-MIT.txt](LICENSES/zerolog-MIT.txt).

## Embedded fonts and artwork

`secretform/Menlo-Regular.ttf` and `secretform/Menlo-Bold.ttf` currently lack an
accompanying license notice in this repository. Their redistribution rights
must be established, or the fonts replaced, before public release. The OTPKI
logo and secret-form branding also require ownership/provenance review. These
assets are not granted new rights merely by adding the root license.

The embedded ICANN CA bundle is trust material; its source and retrieval date
are documented in [dnssec/README.md](dnssec/README.md).

## Module dependencies

Go module dependencies have their own licenses. Versions are recorded in
`go.mod` and `go.sum`; dependency source distributions carry their notices.
Review those terms and any bundled assets when preparing a distribution.

The local [Shamir package](shamir/README.md) was extracted from Vault's
`shamir` package and retains its package-specific [MPL-2.0 license](shamir/LICENSE)
and source notices. The root Vault server module is no longer a dependency.
