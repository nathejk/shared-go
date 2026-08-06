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
