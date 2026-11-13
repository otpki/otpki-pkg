# Common packages

The shared enrollment protocol packages are:

| Package | Purpose |
|---|---|
| `cmp` | CMP wire types, DER encoding/decoding, original protected bytes, legacy PasswordBasedMac, and signature protection. |
| `crmf` | CRMF request types and encoding, original request bytes, signature POP construction/parsing, and signed-input selection. |
| `pkiutil` | ASN.1/SPKI types, signature algorithm mappings, key conversion, and signature verification. |

These packages do not import the OTPKI server or `helpers`. The existing
ASN.1, CRMF, and signature-mapping exports in `helpers` remain compatibility
aliases and wrappers. New protocol consumers should use the focused packages.
The dependency direction is `cmp` → `crmf` → `pkiutil`; CMC and CMP callers keep
their own authorization, trust, transaction state, and protocol error mapping.

This extraction preserves existing supported encodings and algorithm behavior.
It does not add PBMAC1, harden protocol policy, or claim complete CMP/CRMF
conformance. Publication still requires validating the module's existing crypto
fork dependencies for anonymous downstream consumers.

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
