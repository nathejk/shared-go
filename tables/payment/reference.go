package payment

import (
	"fmt"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

// Payment references are shown to humans: they fill a column in the MobilePay
// portal when an operator reconciles a payment, and appear in
// /callback/mobilepay/:ref and /betaling/:ref. A raw UUID spends 36 characters
// saying nothing.
//
//	was:  19e7cc26-f2b9-40be-9aed-449f17b31c9d
//	now:  VBE1HD41RNHB
//
// No prefix. What a payment is for is already recoverable from it — the order it
// names carries the owner and their type — so encoding that again buys nothing
// and costs the one thing a reference is for: being short enough to read and
// compare at a glance.
//
// # Alphabet
//
// Crockford base32 — digits plus uppercase letters except I, L, O and U —
// rather than nanoid's default. The default mixes case and includes - and _,
// which reintroduces every 1/l/I and 0/O confusion in an identifier that gets
// read off a screen, pasted into a support ticket and spelled out over the
// phone. Dropping U keeps accidental profanity out of a customer-facing string.
const referenceAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// referenceLength is the number worth arguing about, and two things set it.
//
// A floor: MobilePay requires at least 8 characters, so 8 would leave no margin
// at all against a provider that counts differently or a future format tweak.
//
// A ceiling on risk: a duplicate reference does not fail loudly — the projector
// upserts on the primary key, so a second payment carrying one already in use
// silently overwrites the first. And because GET /api/payment/:ref is
// unauthenticated, a reference is effectively a bearer token for one payment's
// details, so it must be far too sparse to enumerate.
//
// 12 characters is 60 bits, ~1.2e18 values: at the observed ~700 payments a year
// a collision is a 1-in-4-trillion event, and guessing a valid reference takes
// ~1e15 attempts. Still a third of a UUID's length.
const referenceLength = 12

// newReference returns a fresh payment reference.
//
// The value comes from crypto/rand (via nanoid), never from a counter, a
// timestamp or anything derived from the payment: a guessable reference would
// expose the payer's email through the unauthenticated show endpoint and let a
// stranger trigger the capture callback.
func newReference() (string, error) {
	ref, err := gonanoid.Generate(referenceAlphabet, referenceLength)
	if err != nil {
		return "", fmt.Errorf("payment: generating reference: %w", err)
	}
	return ref, nil
}
