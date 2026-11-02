# Common packages

Go linting uses the same linter set and gofumpt/goimports formatters as
`otpki` and `pkcs11`, with security checks excluded for test code.
Install the pinned tools with `just aqua tools`, then run:

```sh
just format-check # check Go formatting without writing files
just lint         # run golangci-lint
just format       # apply Go formatting
just lint-fix     # apply available automatic lint fixes
just check        # formatting, lint, short tests, and go mod tidy (also runs in CI)
```

Each formatting/lint recipe accepts an optional package target, for example
`just lint ./helpers/...`; the default is `./...`.
