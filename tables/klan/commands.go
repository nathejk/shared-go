package klan

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

type Commands interface {
	RequestMemberCount(context.Context, types.YearSlug, types.TeamID, uint32) (uint32, error)
	Update(context.Context, types.TeamID, UpdateCommand) error
	UpdateTeam(context.Context, types.TeamID, Team) error
	AssignToLok(context.Context, types.TeamID, string) error
	Delete(context.Context, types.TeamID) error

	// AddMember issues a memberId (if the caller didn't supply one) and
	// publishes a single senior.updated event carrying the teamId so the
	// projector upserts a new row. Returns the assigned memberId. This is the
	// only member command that creates an identity.
	AddMember(ctx context.Context, teamID types.TeamID, m Senior) (types.MemberID, error)

	// UpdateMember publishes a single senior.updated event WITHOUT a teamId,
	// so the projector only UPDATEs an existing row and never creates one.
	UpdateMember(ctx context.Context, teamID types.TeamID, m Senior) error

	// DeleteMember publishes a single senior.deleted event.
	DeleteMember(ctx context.Context, teamID types.TeamID, memberID types.MemberID) error
}

type commander struct {
	p cqrs.Publisher
	q Queries
	r repository
}

// RequestMemberCount attempts to reserve seats for the given team.
// It returns the number of seats successfully reserved. If capacity has been
// reached the request is placed on a waiting list and the return value is 0.
//
// The cap is sourced from the product catalogue (participation.klan.stock)
// when WithProductQueries was wired in; otherwise the legacy
// WithTotalMemberCount fallback applies. See repository.go.
func (c *commander) RequestMemberCount(ctx context.Context, year types.YearSlug, teamID types.TeamID, memberCount uint32) (uint32, error) {
	actualMemberCount, err := c.q.RequestedMemberCount(ctx, year)
	if err != nil {
		return 0, err
	}
	cap := c.capacity(ctx, year)
	action := "requested"
	if cap > actualMemberCount {
		action = "reserved"
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.%s.%s.%s", year, types.TeamTypeKlan, teamID, action)))
	msg.SetBody(&messages.NathejkTeamMembersRequested{
		TeamID:      teamID,
		MemberCount: int(memberCount),
	})
	if err := c.p.Publish(msg); err != nil {
		return 0, err
	}
	if action == "requested" {
		return 0, nil
	}
	return memberCount, nil
}

// capacity returns the active seat cap for klan participation in the
// given year. Sources, in priority order:
//
//   - Product catalogue: participation.klan.stock for `year`. NULL stock
//     (unlimited) is treated as no constraint and falls through.
//   - Legacy WithTotalMemberCount option (r.TotalMemberCount).
//
// On any product-query error the function silently falls back to the
// legacy value; capacity gating is non-critical and we'd rather degrade
// to the conservative legacy cap than fail the request.
func (c *commander) capacity(ctx context.Context, year types.YearSlug) uint32 {
	if c.r.Products != nil {
		if p, err := c.r.Products.GetBySKU(ctx, year, "participation.klan"); err == nil && p != nil && p.Stock != nil {
			stock := *p.Stock
			if stock < 0 {
				return 0
			}
			if stock > math.MaxUint32 {
				return math.MaxUint32
			}
			return uint32(stock)
		}
	}
	return c.r.TotalMemberCount
}

type UpdateCommand struct {
	Name      *string `json:"name"`
	GroupName *string `json:"groupName"`
	Korps     *string `json:"korps"`
}

func (c *commander) Update(ctx context.Context, teamID types.TeamID, cmd UpdateCommand) error {
	klan, err := c.q.GetByID(ctx, teamID)
	if err != nil {
		return err
	}

	// Merge: use existing values where the command does not provide an update.
	name := klan.Name
	if cmd.Name != nil {
		name = *cmd.Name
	}
	groupName := klan.Group
	if cmd.GroupName != nil {
		groupName = *cmd.GroupName
	}
	korps := klan.Korps
	if cmd.Korps != nil {
		korps = *cmd.Korps
	}

	// Dirty-check: only publish if something actually changed.
	if name == klan.Name && groupName == klan.Group && korps == klan.Korps {
		return nil
	}

	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.klan.%s.updated", klan.Year, teamID)))
	msg.SetBody(&messages.NathejkKlanUpdated{
		TeamID:    teamID,
		Name:      name,
		GroupName: groupName,
		Korps:     korps,
	})
	if err := c.p.Publish(msg); err != nil {
		return err
	}
	return nil
}

func (c *commander) AssignToLok(ctx context.Context, teamID types.TeamID, lok string) error {
	klan, err := c.q.GetByID(ctx, teamID)
	if err != nil {
		return err
	}

	// Dirty-check: skip if already assigned to the same lok.
	if klan.Lok == lok {
		return nil
	}

	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.klan.%s.assigned", klan.Year, teamID)))
	msg.SetBody(&messages.NathejkKlanAssigned{
		TeamID: teamID,
		Lok:    lok,
	})
	if err := c.p.Publish(msg); err != nil {
		return err
	}
	return nil
}

func (c *commander) Delete(ctx context.Context, teamID types.TeamID) error {
	klan, err := c.q.GetByID(ctx, teamID)
	if err != nil {
		return err
	}

	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.klan.%s.status.changed", klan.Year, teamID)))
	msg.SetBody(&messages.NathejkKlanStatusChanged{
		TeamID: teamID,
		Status: types.SignupStatus("deleted"),
	})
	if err := c.p.Publish(msg); err != nil {
		return err
	}
	return nil
}

// AddMember — see Commands.AddMember.
func (c *commander) AddMember(ctx context.Context, teamID types.TeamID, m Senior) (types.MemberID, error) {
	if m.MemberID == "" {
		m.MemberID = types.MemberID(uuid.New().String())
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.senior.%s.updated", "2026", m.MemberID)))
	// Include teamId so the senior projector's two-phase decode does an
	// INSERT IGNORE for the brand-new member (see senior/consumer.go). This
	// is the create path.
	msg.SetBody(&struct {
		messages.NathejkSeniorUpdated
		TeamID types.TeamID `json:"teamId"`
	}{
		NathejkSeniorUpdated: newSeniorUpdated(m),
		TeamID:               teamID,
	})
	if err := c.p.Publish(msg); err != nil {
		return "", err
	}
	return m.MemberID, nil
}

// UpdateMember — see Commands.UpdateMember.
func (c *commander) UpdateMember(ctx context.Context, teamID types.TeamID, m Senior) error {
	if m.MemberID == "" {
		return fmt.Errorf("UpdateMember: empty memberId")
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.senior.%s.updated", "2026", m.MemberID)))
	// No teamId in the body: the projector skips its INSERT IGNORE branch and
	// performs a pure UPDATE, so a stale/unknown memberId is a no-op rather
	// than resurrecting a member. Update never creates an identity.
	body := newSeniorUpdated(m)
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// DeleteMember — see Commands.DeleteMember.
func (c *commander) DeleteMember(ctx context.Context, teamID types.TeamID, memberID types.MemberID) error {
	if memberID == "" {
		return fmt.Errorf("DeleteMember: empty memberId")
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.senior.%s.deleted", "2026", memberID)))
	msg.SetBody(&messages.NathejkMemberDeleted{
		MemberID: memberID,
		TeamID:   teamID,
	})
	return c.p.Publish(msg)
}

// newSeniorUpdated projects a Senior command value into the wire event body
// shared by UpdateMembers / AddMember / UpdateMember.
func newSeniorUpdated(m Senior) messages.NathejkSeniorUpdated {
	return messages.NathejkSeniorUpdated{
		MemberID:   m.MemberID,
		Name:       m.Name,
		Address:    m.Address,
		PostalCode: m.PostalCode,
		Email:      m.Email,
		Phone:      m.Phone,
		BirthDate:  m.Birthday,
		TShirtSize: m.TShirtSize,
		Diet:       m.Diet,
	}
}

// Team is the team-level slice of an UpdateMembers command.
type Team struct {
	TeamID      types.TeamID `json:"teamId"`
	Name        string       `json:"name"`
	Group       string       `json:"group"`
	Korps       string       `json:"korps"`
	MemberCount int          `json:"memberCount"`
}

// Senior is one member entry on an UpdateMembers command. Setting
// Deleted=true publishes a member-deleted event instead of an update.
type Senior struct {
	MemberID   types.MemberID     `json:"memberId"`
	Deleted    bool               `json:"deleted"`
	Name       string             `json:"name"`
	Address    string             `json:"address"`
	PostalCode string             `json:"postalCode"`
	Email      types.EmailAddress `json:"email"`
	Phone      types.PhoneNumber  `json:"phone"`
	Birthday   types.Date         `json:"birthday"`
	Diet       string             `json:"diet"`
	TShirtSize string             `json:"tshirtSize"`
}

// UpdateTeam projects the team-level slice of a klan save into write events:
// one NathejkKlanUpdated for the team fields, plus the status transitions that
// depend on the global senior cap. Members are NOT touched here — their
// lifecycle is owned by the dedicated AddMember / UpdateMember / DeleteMember
// commands, so a routine team save can never create or delete a senior
// identity (and the old memberCount placeholder rows are gone).
func (c *commander) UpdateTeam(ctx context.Context, teamID types.TeamID, team Team) error {
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.klan.%s.updated", "2026", teamID)))
	msg.SetBody(&messages.NathejkKlanUpdated{
		TeamID:    teamID,
		Name:      team.Name,
		GroupName: team.Group,
		Korps:     team.Korps,
	})
	if err := c.p.Publish(msg); err != nil {
		return err
	}

	klan, _ := c.q.GetByID(ctx, teamID)
	if klan != nil && klan.Status == types.SignupStatusOnHold {
		// The team is on waiting list, do not transition status.
		return nil
	}

	seniorCount, _ := c.q.RequestedSeniorCount(ctx, "2026")
	if seniorCount > 115 {
		statusMsg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.klan.%s.status.changed", "2026", teamID)))
		statusMsg.SetBody(&messages.NathejkKlanStatusChanged{TeamID: teamID, Status: types.SignupStatusOnHold})
		if klan != nil && (klan.Status != types.SignupStatusPay) && (klan.Status != types.SignupStatusPaid) {
			if err := c.p.Publish(statusMsg); err != nil {
				return err
			}
		}
	}
	if klan != nil && klan.Status == "" {
		statusMsg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.klan.%s.status.changed", "2026", teamID)))
		statusMsg.SetBody(&messages.NathejkKlanStatusChanged{TeamID: teamID, Status: types.SignupStatusPay})
		if err := c.p.Publish(statusMsg); err != nil {
			return err
		}
	}
	return nil
}
