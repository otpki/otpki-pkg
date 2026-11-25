// Package cmp provides the shared CMP wire model, DER codec, and existing
// PasswordBasedMac, PBMAC1 and signature protection primitives. It has no dependency on
// the OTPKI server, configuration protobufs, or authorization services.
//
// Callers retain certificate trust, algorithm policy, credential selection,
// nonce validation, transaction state, and protocol error handling.
package cmp
