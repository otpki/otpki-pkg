// Derived from github.com/miekg/dns v1 compatibility helpers.
// Copyright (c) 2009, The Go Authors. Licensed under the BSD 3-Clause License.

package dnssec

import (
	"strings"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
)

func compareDomainName(a, b string) int {
	aLabels := dnsutil.Split(dnsutil.Canonical(a))
	bLabels := dnsutil.Split(dnsutil.Canonical(b))
	common := 0
	for aIndex, bIndex := len(aLabels)-1, len(bLabels)-1; aIndex >= 0 && bIndex >= 0; aIndex, bIndex = aIndex-1, bIndex-1 {
		if !strings.EqualFold(aLabels[aIndex], bLabels[bIndex]) {
			break
		}
		common++
	}
	return common
}

func isSubDomain(parent, child string) bool {
	return compareDomainName(parent, child) == dnsutil.Labels(dnsutil.Canonical(parent))
}

func nsec3Match(record *dns.NSEC3, name string) bool {
	nameHash, ownerHash, _, ok := nsec3Names(record, name)
	return ok && nameHash == ownerHash
}

func nsec3Cover(record *dns.NSEC3, name string) bool {
	nameHash, ownerHash, nextHash, ok := nsec3Names(record, name)
	if !ok {
		return false
	}
	if ownerHash == nextHash && nameHash != ownerHash {
		return true
	}
	if ownerHash > nextHash {
		return nameHash > ownerHash || nameHash < nextHash
	}
	return nameHash >= ownerHash && nameHash < nextHash
}

func nsec3Names(record *dns.NSEC3, name string) (nameHash, ownerHash, nextHash string, ok bool) {
	if record.Hash != dns.SHA1 {
		return "", "", "", false
	}
	ownerLabels := dnsutil.Split(dnsutil.Canonical(record.Hdr.Name))
	if len(ownerLabels) < 2 {
		return "", "", "", false
	}
	ownerZone := dnsutil.Join(ownerLabels[1:]...)
	if !isSubDomain(ownerZone, name) {
		return "", "", "", false
	}
	return strings.ToUpper(dnsutil.NSEC3Name(dnsutil.Canonical(name), record.Salt, record.Iterations)),
		strings.ToUpper(ownerLabels[0]), strings.ToUpper(record.NextDomain), true
}
