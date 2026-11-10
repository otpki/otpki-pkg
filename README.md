# otpki-pkg

Shared Go packages used by OTPKI services and tools. Import the individual
packages you need; this module has no root Go package.

## Packages

| Package | Purpose |
| --- | --- |
| `cryptoenvelope`, `password` | AES-GCM data envelopes, password-based key wrapping, and Argon2id password hashing. |
| `helpers`, `oids` | Certificate and key encodings, X.509 helpers, ASN.1 types, and object identifiers. |
| `dnssec`, `revocation` | DNSSEC validation and certificate revocation checking. See the [DNSSEC guide](dnssec/README.md). |
| `healthcheck`, `oopsie` | Health service adapters and structured errors for Connect and gRPC. |
| `log` | Structured logging and adapters for other logging libraries. |
| `mailer`, `tmpl`, `spa` | Email delivery and templates, text templates, and serving single-page application assets. |
| `maps`, `priorityqueue`, `uris` | Typed concurrent maps, priority queues, and URI handling. |
| `shamir` | Shamir secret sharing with compatibility for existing Vault-format shares. See the [package guide](shamir/README.md). |
| `secretform` | Secret-form PDFs, QR codes, and key check values. |

The [Docker build helper](tools/docker/buildctl/README.md) lives under
`tools/docker/buildctl`.

## Requirements and installation

The minimum Go version is 1.27.3, as declared in `go.mod`. Once the publication
prerequisites below are complete, install a package with:

```sh
go get github.com/otpki/otpki-pkg/cryptoenvelope@latest
```

Import paths start with `github.com/otpki/otpki-pkg/`, for example
`github.com/otpki/otpki-pkg/cryptoenvelope`.

**Publication status:** IOGO and CIRCL are public; clean anonymous installation
of the complete module still needs verification. Bundled font and artwork
redistribution needs review, and the final PKCS7 fork migration remains
coordinated with its release.

## Development

Install Go, `just`, and `curl`, then install pinned tools:

```sh
just tools
just check        # formatting, lint, short tests, and module tidy (also runs in CI)
just test         # full test suite
just vulncheck    # known vulnerabilities in reachable code
```

Go linting uses the same linter set and gofumpt/goimports formatters as `otpki`
and `pkcs11`, with security checks excluded for test code.

```sh
just format-check # check Go formatting without writing files
just lint         # run golangci-lint
just format       # apply Go formatting
just lint-fix     # apply available automatic lint fixes
```

`format`, `format-check`, and `vulncheck` follow the OTPKI command names. `lint`
follows `pkcs11`, with `lint-go` as an alias matching the OTPKI name. `tidy` runs
module tidying. CI runs formatting, lint, short tests, and module tidying as
separate steps. Vulnerability scanning runs in a separate advisory job with
`continue-on-error` enabled.

Each formatting/lint recipe accepts an optional package target, for example
`just lint ./helpers/...`; the default is `./...`.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution workflow and
[SECURITY.md](SECURITY.md) for private vulnerability reporting.

## License

Unless a file or directory carries separate terms, this project is licensed
under the [Apache License 2.0](LICENSE). Third-party code and assets retain their
own licenses and notices; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
The project license does not establish redistribution rights for bundled fonts
or artwork with unresolved provenance.
