package types

// SectionType identifies which kind of section a crew member belongs to.
// The values are the persisted, wire-level representations and must not be
// changed without migrating existing data.
type SectionType string

const (
	// SectionTypeBandit is the section of banditter roaming the course.
	SectionTypeBandit SectionType = "bandit"
	// SectionTypeActor is the section of gøglere putting on acts for the patrols.
	SectionTypeActor SectionType = "gøgler"
	// SectionTypePost is the section manning a fixed checkpoint along the course.
	SectionTypePost SectionType = "post"
	// SectionTypeCrew is the general crew section, used for everyone not
	// covered by a more specific section type.
	SectionTypeCrew SectionType = "crew"
)

// SectionFlag marks a capability of a section. A section can carry any number
// of flags, and the flags control how the section may be used elsewhere in the
// system. Like SectionType, the values are persisted as-is.
type SectionFlag string

const (
	// SectionFlagSelfAssignable allows a crew member to join the section on
	// their own, without being assigned to it by someone else.
	SectionFlagSelfAssignable SectionFlag = "selfassignable"
	// SectionFlagSosAssignable allows Trailside assistance (Nødtelefonen) to
	// assign SOS tasks to the section.
	SectionFlagSosAssignable SectionFlag = "sosassignable"
	// SectionFlagDispatchable makes the section available to the dispatch
	// interface in the HQ repo, which connects SOS tasks to tours assigned to
	// the section. Such sections most often also have a vehicle at their
	// disposal.
	SectionFlagDispatchable SectionFlag = "dispatchable"
)
