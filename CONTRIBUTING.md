# Contributing

## Requirements

- Go 1.27.2 or newer, matching the minimum in `go.mod`.
- `just` and `curl` on `PATH`. Pinned development tools are installed under
  `.tool/` by `just tools`.

Public builds must work without organization credentials.

## Workflow

1. Open an issue first for changes beyond a small fix so the direction can be
   agreed before implementation.
2. Make the change on a branch or in a fork.
3. Run `just check`: formatting, lint, short tests, and `go mod tidy`.
4. Run `just test` for the full test suite and `just vulncheck` for known
   vulnerabilities in reachable code.
5. Review `git diff`, including module changes, then open a PR with a short
   summary and a test plan.

`just check` checks formatting without rewriting Go files. Use `just format`
when you want to apply formatting, and `just lint-fix` to apply available lint
fixes. CI also checks that validation leaves the checkout unchanged.

## Package and API changes

This module is a collection of packages, not a single application. Keep each
package focused and avoid unnecessary dependencies between packages. Preserve
exported names and behavior unless a breaking change is deliberate and clearly
explained. Add regression tests for bug fixes and document error behavior.

Keep production credentials, private keys, customer data, and internal endpoints
out of fixtures and logs. Use generated test keys and synthetic data. Preserve
upstream copyright and license notices when adapting code or adding assets; see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Security

Do not report vulnerabilities through public issues or PRs. Follow
[SECURITY.md](SECURITY.md).
