# DNSSEC validator

This package is derived from the focused `dnssec` package in
[`github.com/folbricht/routedns`](https://github.com/folbricht/routedns), at
commit `2d34b9565853910155a89c499556175a72b1f47b`. It was adapted to use the v2
`codeberg.org/miekg/dns` API. See [LICENSE](LICENSE) for the upstream BSD
3-Clause license. The small compatibility helpers adapted from miekg/dns v1
are covered by [LICENSE-miekg-dns](LICENSE-miekg-dns).

## IANA root trust anchors

The package starts with embedded DNSSEC root anchors. Calling
`RefreshIANARootTrustAnchors` downloads IANA's XML publication and detached CMS
signature, verifies the signature against the embedded ICANN CA bundle, checks
the published validity windows and DNSKEY digests, and then atomically replaces
the in-memory defaults. A failed refresh leaves the previous anchors active.
This is a startup refresh mechanism, not an RFC 5011 rollover implementation.

The pinned CA bundle was retrieved from
`https://data.iana.org/root-anchors/icannbundle.pem` on 2026-09-14 and contains
the original ICANN Root CA and ICANN Root CA v2. It must be reviewed as an
out-of-band trust decision when IANA changes its signing CA hierarchy.
