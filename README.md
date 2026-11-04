# Common packages

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
