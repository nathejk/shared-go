package spejder

import (
	"fmt"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
)

type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() (subjs []cqrs.Subject) {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK.*.spejder.*.updated"),
		cqrs.SubjectFromStr("NATHEJK.*.spejder.*.deleted"),
		cqrs.SubjectFromStr("NATHEJK.*.spejder.*.reassigned"),
		cqrs.SubjectFromStr("NATHEJK:*.patrulje.*.started"),
	}
}

func (c *consumer) HandleMessage(msg cqrs.Message) error {
	switch true {
	case msg.Subject().Match("nathejk.*.spejder.*.added"):
		var body messages.NathejkMemberAdded
		if err := msg.Body(&body); err != nil {
			return err
		}
		query := `INSERT IGNORE INTO spejder (memberId, year, teamId, createdAt) VALUES (%q,%q,%q,%q)`
		args := []any{
			body.MemberID,
			msg.Subject().Parts()[1],
			body.TeamID,
			msg.Time(),
		}
		return c.w.Consume(fmt.Sprintf(query, args...))

	case msg.Subject().Match("nathejk.*.spejder.*.updated"):
		var legacy messages.NathejkMemberAdded
		if err := msg.Body(&legacy); err != nil {
			return err
		}
		if legacy.TeamID != "" {
			query := `INSERT IGNORE INTO spejder (memberId, year, teamId, createdAt) VALUES (%q,%q,%q,%q)`
			args := []any{
				legacy.MemberID,
				msg.Subject().Parts()[1],
				legacy.TeamID,
				msg.Time(),
			}
			if err := c.w.Consume(fmt.Sprintf(query, args...)); err != nil {
				return err
			}
		}
		var body messages.NathejkScoutUpdated
		if err := msg.Body(&body); err != nil {
			return err
		}
		returning := "0"
		if body.Returning {
			returning = "1"
		}
		query := `UPDATE spejder SET
			name=%q,
			address=%q,
			postalCode=%q,
			city=%q,
			email=%q,
			phone=%q,
			phoneParent=%q,
			birthday=%q,
			tshirtSize=%q,
			` + "`returning`=%s," + `
		 	updatedAt=%q
			WHERE memberId = %q`
		args := []any{
			body.Name,
			body.Address,
			body.PostalCode,
			body.City,
			body.Email,
			body.Phone,
			body.PhoneContact,
			body.BirthDate,
			body.TShirtSize,
			returning,
			msg.Time(),
			body.MemberID,
		}
		return c.w.Consume(fmt.Sprintf(query, args...))
	case msg.Subject().Match("nathejk.*.spejder.*.reassigned"):
		var body messages.NathejkMemberReassigned
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.MemberID == "" || body.ToTeamID == "" {
			return nil
		}
		// The only branch in this file that writes teamId. spejder.*.updated
		// deliberately does not, so a pre-race reassignment is the single way a
		// member's team changes on the roster.
		//
		// Scoped by year *and* memberId, unlike the UPDATE above it: the primary
		// key is (year, memberId) and member ids are not year-scoped, so a
		// reassignment in one season would otherwise rewrite the same member's
		// row in another.
		//
		// Idempotent by construction — it sets an absolute value rather than
		// moving one — so a replay lands on the same row content.
		query := `UPDATE spejder SET teamId=%q, updatedAt=%q WHERE year=%q AND memberId=%q`
		args := []any{
			body.ToTeamID,
			msg.Time(),
			msg.Subject().Parts()[1],
			body.MemberID,
		}
		return c.w.Consume(fmt.Sprintf(query, args...))

	case msg.Subject().Match("nathejk.*.spejder.*.deleted"):
		var body messages.NathejkScoutDeleted
		if err := msg.Body(&body); err != nil {
			return err
		}
		return c.w.Consume(fmt.Sprintf("DELETE FROM spejder WHERE memberId=%q", body.MemberID))
	case msg.Subject().Match("nathejk.*.patrulje.*.started"):
		var body messages.NathejkTeamStarted
		if err := msg.Body(&body); err != nil {
			return err
		}
		for _, member := range body.Members {
			query := `UPDATE spejder SET phone=%q, phoneParent=%q WHERE memberId=%q`
			args := []any{member.Phone, member.PhoneGuardian, member.MemberID}

			if err := c.w.Consume(fmt.Sprintf(query, args...)); err != nil {
				return err
			}
		}
	}
	return nil
}
