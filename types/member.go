package types

// MemberStatus is where a member is in the lifecycle of one Nathejk, from
// signup through the race to leaving our care.
//
// It answers one question — "what is true of this member right now?" — and the
// states are exclusive: a member is in exactly one of them. Anything that is
// several facts at once (a seat is paid for AND the member has a t-shirt size)
// belongs in its own field, not here.
//
// # Lifecycle
//
//	registered ──seat assigned──> seated ──race starts──> racing ──walks it──> finished
//	                                                        │
//	                                     calls trailside assistance
//	                                                        ↓
//	                                                     waiting
//	                                                        │ a car collects them
//	                                                        ↓
//	                                                     transit
//	                                                        │ handed over at HQ
//	                                                        ↓
//	                                                    sheltered
//	                                                     ╱      ╲
//	                              a guardian collects them      their own team finishes
//	                                                   ↓          ↓
//	                                              released      reunited
//
// Before the race the team buys seats and assigns them, so a member exists
// before a seat is paid for — which is why registered and seated are separate:
// the gap between them is the team's outstanding order.
//
// From racing onwards there are two ways out, and they are not
// interchangeable. A member either walks the route to the end (finished) or
// leaves it and enters our care (waiting, transit, sheltered) until somebody
// takes them off our hands (released, reunited). The withdrawal route is
// one-way: leaving it back onto the route is not a transition we model, so
// finished means what it says — see CanFinish.
//
// # Terminal states
//
// finished, released and reunited are ends; everything else means we are still
// responsible for somebody, which is what makes a stale status dangerous rather
// than untidy. See InOurCare for the count that has to reach zero before the
// organisers can go home.
//
// # Persisted values
//
// These strings live in the spejderstatus projection and on the wire, so
// changing one is a data migration, not a rename. The values that predate this
// set and may still be in old rows:
//
//	REGISTERED, STARTED   → registered, racing
//	active                → racing
//	emergency             → waiting
//	waiting, transit      → unchanged
//	hq                    → sheltered
//	out                   → released
type MemberStatus string

const (
	// MemberStatusNone is the zero value: no status recorded. A member read
	// from a projection that predates status tracking has this.
	MemberStatusNone MemberStatus = ""

	// MemberStatusRegistered means the member is on the team's roster but has
	// no seat yet. The team has either not bought enough seats or not assigned
	// the one it bought; until then the member is an intention, not a
	// participant.
	MemberStatusRegistered MemberStatus = "registered"

	// MemberStatusSeated means a paid seat is assigned to this member: they are
	// coming. Adding another member means buying another seat, so the count of
	// seated members is what the team has actually paid for.
	MemberStatusSeated MemberStatus = "seated"

	// MemberStatusRacing means the member signed in at the start and is on the
	// trail. This is the only state in which a member counts towards their
	// team's strength on the route, and the only one from which they can
	// finish.
	MemberStatusRacing MemberStatus = "racing"

	// MemberStatusFinished means the member walked the route to the end under
	// their own steam.
	//
	// Reserved for exactly that. A member who left the route and was driven in
	// has not finished, however late they dropped out and whether or not they
	// were back with their team by the time it crossed the line — those end at
	// reunited instead. Keeping the two apart is what lets finished be counted
	// as an achievement rather than as attendance.
	MemberStatusFinished MemberStatus = "finished"

	// MemberStatusWaiting means the member has left the route and is waiting to
	// be collected: they (or their team) called trailside assistance and a car
	// has yet to reach them.
	//
	// The team may not continue until the member is collected, so this state
	// blocks the whole patrol — it is the one worth an alarm on a dashboard
	// when it lasts too long.
	MemberStatusWaiting MemberStatus = "waiting"

	// MemberStatusTransit means the member is in one of our cars. The car is
	// not necessarily driving to HQ: it may be collecting members from other
	// teams first, so being in transit says who holds the member, not how long
	// until they arrive.
	MemberStatusTransit MemberStatus = "transit"

	// MemberStatusSheltered means the member has been handed over at HQ and is
	// in our care there — put to bed if it is the middle of the night, waiting
	// in the warm if somebody is already on the way for them. Which of the two
	// it is depends on the hour, not on anything we track, so it is one state.
	MemberStatusSheltered MemberStatus = "sheltered"

	// MemberStatusReunited means the member's own team reached the finish and
	// the member has been handed back to it. They are still in the race area,
	// now in their leaders' charge rather than ours, and they go home with the
	// team.
	//
	// This is the ordinary ending for a member who dropped out late: too far in
	// for a guardian to be called out, close enough to the end that the team
	// arrives before anybody needs to. It is deliberately not finished — see
	// MemberStatusFinished.
	MemberStatusReunited MemberStatus = "reunited"

	// MemberStatusReleased means the member has been handed over to a guardian
	// who came for them, and we no longer track them. The other ending for a
	// member who left the route, and the one that happens during the night
	// rather than at the finish.
	MemberStatusReleased MemberStatus = "released"
)

// Valid reports whether s is a status this version of the code knows.
//
// MemberStatusNone is deliberately excluded: a member whose status is unset is
// readable but not a valid thing to publish or store.
func (s MemberStatus) Valid() bool {
	switch s {
	case MemberStatusRegistered,
		MemberStatusSeated,
		MemberStatusRacing,
		MemberStatusFinished,
		MemberStatusWaiting,
		MemberStatusTransit,
		MemberStatusSheltered,
		MemberStatusReunited,
		MemberStatusReleased:
		return true
	}
	return false
}

// CanFinish reports whether a member in this status may still finish the race.
//
// Only a member who is still on the route can, which makes this a one-line
// guard against the mistake the finish line invites: a member who was driven to
// HQ and handed back to their team when it arrived is standing in the same
// place as the finishers, and marking them finished would quietly turn a
// withdrawal into a completed route. They end at reunited.
func (s MemberStatus) CanFinish() bool {
	return s == MemberStatusRacing
}

// InOurCare reports whether Nathejk is currently responsible for the member's
// physical whereabouts — from the moment they leave the route to the moment
// somebody else takes charge of them.
//
// This is the count that has to reconcile with reality before the organisers
// can go home, which is why it is a method rather than a comparison spelled out
// at each call site. Members who are racing are not included: their team is
// with them and their whereabouts are the route's business, not the car pool's.
func (s MemberStatus) InOurCare() bool {
	switch s {
	case MemberStatusWaiting, MemberStatusTransit, MemberStatusSheltered:
		return true
	}
	return false
}
