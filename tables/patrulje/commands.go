package patrulje

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// Commands is the patrulje write-side API. Methods publish domain events
// onto the stream; the patrulje projection (consumer.go) is the read-side.
type Commands interface {
	Update(ctx context.Context, teamID types.TeamID, team Team, contact Contact) error
	AssignNumber(ctx context.Context, teamID types.TeamID) error

	// AddMember issues a memberId (if the caller didn't supply one) and
	// publishes a single spejder.updated event carrying the teamId so the
	// projector upserts a new row. Returns the assigned memberId. This is the
	// only member command that creates an identity.
	AddMember(ctx context.Context, teamID types.TeamID, m Spejder) (types.MemberID, error)

	// UpdateMember publishes a single spejder.updated event WITHOUT a teamId,
	// so the projector only UPDATEs an existing row and never creates one. The
	// memberId must be non-empty.
	UpdateMember(ctx context.Context, teamID types.TeamID, m Spejder) error

	// DeleteMember publishes a single spejder.deleted event.
	DeleteMember(ctx context.Context, teamID types.TeamID, memberID types.MemberID) error
}

// Team is the team-level slice of an UpdatePatrulje command.
type Team struct {
	TeamID          types.TeamID `json:"teamId"`
	Name            string       `json:"name"`
	Group           string       `json:"group"`
	Korps           string       `json:"korps"`
	AdventureLigaID string       `json:"liga"`
}

// Contact is the contact-person slice of an UpdatePatrulje command.
type Contact struct {
	TeamID     types.TeamID       `json:"teamId"`
	Name       string             `json:"name"`
	Address    string             `json:"address"`
	PostalCode string             `json:"postal"`
	Email      types.EmailAddress `json:"email"`
	Phone      types.PhoneNumber  `json:"phone"`
	Role       string             `json:"role"`
}

// Spejder is one member entry on an UpdatePatrulje command. Setting
// Deleted=true publishes a member-deleted event instead of an update.
type Spejder struct {
	MemberID     types.MemberID     `json:"memberId"`
	Deleted      bool               `json:"deleted"`
	Name         string             `json:"name"`
	Address      string             `json:"address"`
	PostalCode   string             `json:"postalCode"`
	Email        types.EmailAddress `json:"email"`
	Phone        types.PhoneNumber  `json:"phone"`
	PhoneContact types.PhoneNumber  `json:"phoneContact"`
	Birthday     types.Date         `json:"birthday"`
	TShirtSize   string             `json:"tshirtSize"`
}

type commander struct {
	p cqrs.Publisher
	q *querier
}

// Update publishes a NathejkTeamUpdated for the team / contact slice. Members
// are no longer touched here — their lifecycle is owned by the dedicated
// AddMember / UpdateMember / DeleteMember commands, so a routine team save can
// never create or delete a member identity.
func (c *commander) Update(ctx context.Context, teamID types.TeamID, team Team, contact Contact) error {
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.patrulje.%s.updated", "2026", teamID)))
	msg.SetBody(&messages.NathejkTeamUpdated{
		TeamID:            teamID,
		Type:              types.TeamTypePatrulje,
		Name:              team.Name,
		GroupName:         team.Group,
		Korps:             team.Korps,
		AdvspejdNumber:    team.AdventureLigaID,
		ContactName:       contact.Name,
		ContactAddress:    contact.Address,
		ContactPostalCode: contact.PostalCode,
		ContactEmail:      contact.Email,
		ContactPhone:      contact.Phone,
		ContactRole:       contact.Role,
	})
	return c.p.Publish(msg)
}

// AddMember — see Commands.AddMember.
func (c *commander) AddMember(ctx context.Context, teamID types.TeamID, m Spejder) (types.MemberID, error) {
	if m.MemberID == "" {
		m.MemberID = types.MemberID(uuid.New().String())
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.spejder.%s.updated", "2026", m.MemberID)))
	// Include teamId so the projector's two-phase decode does an INSERT IGNORE
	// for the brand-new member (see spejder/consumer.go). This is the create
	// path.
	msg.SetBody(&struct {
		messages.NathejkScoutUpdated
		TeamID types.TeamID `json:"teamId"`
	}{
		NathejkScoutUpdated: newScoutUpdated(m),
		TeamID:              teamID,
	})
	if err := c.p.Publish(msg); err != nil {
		return "", err
	}
	return m.MemberID, nil
}

// UpdateMember — see Commands.UpdateMember.
func (c *commander) UpdateMember(ctx context.Context, teamID types.TeamID, m Spejder) error {
	if m.MemberID == "" {
		return fmt.Errorf("UpdateMember: empty memberId")
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.spejder.%s.updated", "2026", m.MemberID)))
	// No teamId in the body: the projector skips its INSERT IGNORE branch and
	// performs a pure UPDATE, so a stale/unknown memberId is a no-op rather
	// than resurrecting a member. Update never creates an identity.
	body := newScoutUpdated(m)
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// DeleteMember — see Commands.DeleteMember.
func (c *commander) DeleteMember(ctx context.Context, teamID types.TeamID, memberID types.MemberID) error {
	if memberID == "" {
		return fmt.Errorf("DeleteMember: empty memberId")
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.spejder.%s.deleted", "2026", memberID)))
	msg.SetBody(&messages.NathejkMemberDeleted{
		MemberID: memberID,
		TeamID:   teamID,
	})
	return c.p.Publish(msg)
}

// newScoutUpdated projects a Spejder command value into the wire event body
// shared by Update / AddMember / UpdateMember.
func newScoutUpdated(m Spejder) messages.NathejkScoutUpdated {
	return messages.NathejkScoutUpdated{
		MemberID:     m.MemberID,
		Name:         m.Name,
		Address:      m.Address,
		PostalCode:   m.PostalCode,
		Email:        m.Email,
		Phone:        m.Phone,
		PhoneContact: m.PhoneContact,
		BirthDate:    m.Birthday,
		TShirtSize:   m.TShirtSize,
	}
}

// AssignNumber finds the highest team number in use and publishes a
// NathejkPatrolNumberAssigned event for `teamID` set to that-number+1
// (starting at 1 if none have been assigned yet).
func (c *commander) AssignNumber(ctx context.Context, teamID types.TeamID) error {
	last, err := c.q.GetLastWithNumber(ctx)
	nr := 1
	if err == nil && last != nil && last.TeamNumber != "" {
		i, perr := strconv.Atoi(last.TeamNumber)
		if perr != nil {
			return fmt.Errorf("unable to find next number %#v", perr)
		}
		nr = i + 1
	} else if err != nil && !errors.Is(err, tables.ErrRecordNotFound) {
		return err
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK.%s.patrulje.%s.numberassigned", "2026", teamID)))
	msg.SetBody(&messages.NathejkPatrolNumberAssigned{
		TeamID:     teamID,
		TeamNumber: fmt.Sprintf("%d", nr),
	})
	return c.p.Publish(msg)
}
