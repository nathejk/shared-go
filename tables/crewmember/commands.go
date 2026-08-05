package crewmember

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// Commands is the write-side interface for crew members.
type Commands interface {
	Register(ctx context.Context, year types.YearSlug, name string, phone types.PhoneNumber, email types.EmailAddress) (types.UserID, error)
	Update(ctx context.Context, year types.YearSlug, userID types.UserID, f UpdateFields) error
	AssignSection(ctx context.Context, year types.YearSlug, userID types.UserID, section types.Slug) error
	Delete(ctx context.Context, year types.YearSlug, userID types.UserID) error
}

// UpdateFields is the editable slice of a crew member carried by Update.
// Section membership is deliberately excluded — it has its own command
// (AssignSection) and its own event, so a details edit never clobbers the
// assignment and vice-versa.
type UpdateFields struct {
	Name        string
	Phone       types.PhoneNumber
	Email       types.EmailAddress
	MedlemNr    string
	Group       string
	Corps       types.CorpsSlug
	Diet        string
	Additionals map[string]any
}

type commander struct {
	p cqrs.Publisher
	q Queries
}

// Register creates a new crew member with a generated UserID and publishes
// NathejkCrewMemberRegistered.
func (c commander) Register(ctx context.Context, year types.YearSlug, name string, phone types.PhoneNumber, email types.EmailAddress) (types.UserID, error) {
	if !year.Valid() {
		return "", fmt.Errorf("invalid year slug %q", year)
	}
	if name == "" {
		return "", errors.New("crew member name is required")
	}
	userID := types.UserID(uuid.New().String())
	body := messages.NathejkCrewMemberRegistered{
		UserID: userID,
		Name:   name,
		Phone:  phone,
		Email:  email,
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.crewmember.%s.registered", year, userID),
	))
	msg.SetBody(&body)
	if err := c.p.Publish(msg); err != nil {
		return "", err
	}
	return userID, nil
}

// Update publishes NathejkCrewMemberUpdated with the editable crew-member
// fields. The projection's "updated" handler upserts on userId, so Update
// also serves to create a row for a userId that was minted elsewhere (e.g.
// the signup flow) but not yet registered through this package.
func (c commander) Update(ctx context.Context, year types.YearSlug, userID types.UserID, f UpdateFields) error {
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if userID == "" {
		return errors.New("userId is required")
	}
	body := messages.NathejkCrewMemberUpdated{
		UserID:      userID,
		Name:        f.Name,
		Phone:       f.Phone,
		Email:       f.Email,
		MedlemNr:    f.MedlemNr,
		Group:       f.Group,
		Corps:       f.Corps,
		Diet:        f.Diet,
		Additionals: f.Additionals,
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.crewmember.%s.updated", year, userID),
	))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// AssignSection publishes NathejkCrewMemberSectionAssigned. Passing an empty
// slug is the way to unassign a crew member. Assigning a different section
// implicitly unassigns the current one.
func (c commander) AssignSection(ctx context.Context, year types.YearSlug, userID types.UserID, section types.Slug) error {
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if userID == "" {
		return errors.New("userId is required")
	}
	if section != "" && !section.Valid() {
		return fmt.Errorf("invalid section slug %q", section)
	}
	// Tolerate a not-yet-projected crew member: the section.assigned
	// consumer upserts on userId, so assigning before the row lands (e.g.
	// racing the signup projection) still produces a correct row. Only the
	// no-op optimisation below depends on the row already existing.
	existing, err := c.q.GetByID(ctx, userID)
	if err != nil && !errors.Is(err, tables.ErrRecordNotFound) {
		return err
	}
	if existing != nil && existing.SectionSlug == section {
		return nil
	}

	body := messages.NathejkCrewMemberSectionAssigned{
		UserID:      userID,
		SectionSlug: section,
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.crewmember.%s.section.assigned", year, userID),
	))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// Delete publishes NathejkCrewMemberDeleted (soft delete in the read model).
func (c commander) Delete(ctx context.Context, year types.YearSlug, userID types.UserID) error {
	body := messages.NathejkCrewMemberDeleted{UserID: userID}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.crewmember.%s.deleted", year, userID),
	))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}
