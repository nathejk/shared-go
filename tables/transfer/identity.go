package transfer

import (
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"github.com/nathejk/shared-go/types"
)

// Everything a transfer publishes is named by a deterministic function of the
// transfer itself — no UUIDs minted at handling time, no timestamps in an id.
//
// Two reasons, and the second is the one that bites:
//
//   - A repeated command (a double-submitted form, an operator pressing the button
//     twice) must produce the same transfer rather than a second one. With derived
//     ids the second run re-publishes the same events for the same order ids and
//     line ids, which the projectors upsert; with fresh ids it would credit the
//     origin twice and charge the destination twice.
//   - A replayed event log must produce one transfer, not two. The events carry
//     their ids, so replay is safe either way — but only derived ids make the
//     *command* safe to run again, and a rebuild that re-ran anything would
//     otherwise diverge from the log.
//
// The trade-off is deliberate: moving the same member A → B twice, with a move
// back in between, reuses the first transfer's names. That collapses into a single
// transfer whose lines and amounts are the same either way, which is a far better
// failure mode than double-crediting anybody.

// namespaceSeatTransfer is the UUIDv5 namespace for transfer order ids. A fixed
// random UUID: it only has to be stable and not collide with another namespace.
var namespaceSeatTransfer = uuid.MustParse("6f2e5b3a-9c41-4f7d-8b2a-1d5e7c9a4b60")

// referenceAlphabet is Crockford base32 — digits and uppercase letters except I,
// L, O and U — the same alphabet payment references use, for the same reason: a
// transfer id ends up in a payment reference, which gets read off a screen and
// spelled out over the phone.
const referenceAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// transferIDLength matches the length of a provider payment reference, so the two
// look alike where they appear side by side.
const transferIDLength = 12

// Identity is the deterministic naming of one transfer: the two orders it creates
// and the two payments that cover them.
//
// Exported, and derivable by anyone with the same inputs, so a caller can find a
// transfer's orders again without storing a mapping — and so a UI can label the
// pair. See IdentityOf.
type Identity struct {
	// TransferID names the transfer itself. It appears in every line id on both
	// orders (see LineID), which is what makes the two halves discoverable from
	// either one.
	TransferID string

	// CreditOrderID is the origin's order: the same lines, negated.
	CreditOrderID string

	// ChargeOrderID is the destination's order: the same lines, positive.
	ChargeOrderID string

	// CreditReference / ChargeReference name the two internal-transfer payments.
	// Prefixed and hyphenated so they cannot collide with a provider reference,
	// which is twelve characters of the alphabet above and nothing else.
	CreditReference string
	ChargeReference string
}

// IdentityOf derives the names of the transfer moving memberID from fromTeamID to
// toTeamID in the given year.
//
// The year is part of the key: member and team ids are not year-scoped, so the
// same move in two seasons must not resolve to the same orders.
func IdentityOf(year types.YearSlug, memberID types.MemberID, fromTeamID, toTeamID types.TeamID) Identity {
	key := fmt.Sprintf("%s|%s|%s|%s", year, memberID, fromTeamID, toTeamID)
	id := token(key)
	return Identity{
		TransferID:      id,
		CreditOrderID:   derivedUUID("credit|" + key),
		ChargeOrderID:   derivedUUID("charge|" + key),
		CreditReference: "T-" + id + "-C",
		ChargeReference: "T-" + id + "-D",
	}
}

// LineID is the id of the i-th line on either half of the transfer.
//
// The same suffix on both halves on purpose: credit line i and charge line i are
// the two sides of the same moved line, so finance can pair them without matching
// on amounts. The transfer id is embedded so that either order's lines identify
// the transfer they belong to, which is what lets a caller find the pair with a
// single prefix match instead of a new column.
func (i Identity) LineID(n int) string {
	return fmt.Sprintf("transfer:%s:%d", i.TransferID, n)
}

// derivedUUID is a UUIDv5 over the namespace, so a transfer's order ids look like
// every other order id in the system while still being a function of the transfer.
func derivedUUID(name string) string {
	return uuid.NewSHA1(namespaceSeatTransfer, []byte(name)).String()
}

// token renders a short, human-readable id from a hash of key.
//
// Five bits per character out of a SHA-256 digest: 60 bits over twelve characters,
// the same budget a provider reference has, which is far beyond any plausible
// number of transfers.
func token(key string) string {
	sum := sha256.Sum256([]byte(key))
	out := make([]byte, transferIDLength)
	for i := range out {
		out[i] = referenceAlphabet[sum[i]%byte(len(referenceAlphabet))]
	}
	return string(out)
}
