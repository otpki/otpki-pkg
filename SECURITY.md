# Security

## Reporting a vulnerability

Please do not open a public issue or PR for a suspected vulnerability. Use
[GitHub private vulnerability reporting](https://github.com/otpki/otpki-pkg/security/advisories/new)
so maintainers can investigate and prepare a fix before details are public.
Maintainers must enable private vulnerability reporting before publication.

Include the affected package and module version or commit, a minimal
reproduction, the expected security boundary, and the impact. Use synthetic
credentials and keys. Do not attach live secrets or customer data. If you are
unsure whether an issue is security-sensitive, report privately first.

## Supported versions

Security fixes target the latest revision on `master`. A backport policy for
older versions has not been established. Include your exact module version in
reports and review release notes before upgrading.

## Security boundaries

- `cryptoenvelope`, `password`, and key helpers handle sensitive material.
  Callers own key storage, password policy, access control, and the lifetime of
  caller-owned buffers. Go cannot guarantee removal of every copy of a secret
  from memory, logs, or crash dumps.
- SHA-1 identifiers and legacy encrypted PEM import exist for interoperability.
  Their presence is not a recommendation to use those formats for new security
  designs. Prefer modern authenticated encryption and PKCS#8 key formats.
- Certificate parsing and key conversion do not, by themselves, establish
  certificate trust. Applications must verify identity, chain policy, validity,
  and revocation as appropriate for their use case.
- `dnssec` uses embedded trust anchors and supports verified IANA refreshes.
  Review changes to the pinned trust material as a separate trust decision;
  see [the package guide](dnssec/README.md).
- Logging adapters, mail delivery, templates, secret-form PDFs, and QR codes can
  expose information deliberately supplied by callers. Treat generated secret
  forms as secrets and keep credentials and key material out of diagnostics.
- The Docker build helper executes local build commands and can forward a
  GitHub token through BuildKit secrets. Use trusted build contexts and
  Dockerfiles, and grant tokens only the permissions the build requires.
