package senior

import (
	"fmt"
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"

	_ "embed"
)

type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() []cqrs.Subject {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK.*.senior.*.updated"),
		cqrs.SubjectFromStr("NATHEJK.*.senior.*.deleted"),
		cqrs.SubjectFromStr("NATHEJK.*.bandit.*.armNumber.assigned"),
	}
}

func (c *consumer) HandleMessage(msg cqrs.Message) error {
	switch true {
	case msg.Subject().Match("nathejk.*.senior.*.updated"):
		// Two-phase decode mirroring spejder/consumer.go: the legacy
		// NathejkMemberAdded shape (with TeamID) is decoded first to
		// opportunistically INSERT the row when the team association is
		// known on the event. The new NathejkSeniorUpdated shape carries
		// only the editable senior fields and is then used to UPDATE.
		var legacy messages.NathejkMemberAdded
		if err := msg.Body(&legacy); err != nil {
			return err
		}
		if legacy.TeamID != "" {
			query := `INSERT IGNORE INTO senior (memberId, year, teamId, createdAt) VALUES (%q,%q,%q,%q)`
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
		var body messages.NathejkSeniorUpdated
		if err := msg.Body(&body); err != nil {
			return err
		}
		query := `UPDATE senior SET
			name=%q,
			address=%q,
			postalCode=%q,
			city=%q,
			email=%q,
			phone=%q,
			birthday=%q,
			tshirtSize=%q,
			diet=%q,
			updatedAt=%q
			WHERE memberId = %q`
		args := []any{
			body.Name,
			body.Address,
			body.PostalCode,
			body.City,
			body.Email,
			body.Phone,
			body.BirthDate,
			body.TShirtSize,
			body.Diet,
			msg.Time(),
			body.MemberID,
		}
		if err := c.w.Consume(fmt.Sprintf(query, args...)); err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}

	case msg.Subject().Match("nathejk.*.senior.*.deleted"):
		var body messages.NathejkMemberDeleted
		if err := msg.Body(&body); err != nil {
			return err
		}
		query := "DELETE FROM senior WHERE memberId=%q"
		args := []any{body.MemberID}
		err := c.w.Consume(fmt.Sprintf(query, args...))
		if err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}

	case msg.Subject().Match("NATHEJK.*.bandit.*.armNumber.assigned"):
		var body messages.NathejkLokArmNumberAssigned
		if err := msg.Body(&body); err != nil {
			return err
		}
		query := "UPDATE senior SET armNumber=%q WHERE memberId=%q"
		args := []any{
			body.ArmNumber,
			msg.Subject().Parts()[3],
		}
		err := c.w.Consume(fmt.Sprintf(query, args...))
		if err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}

	default:
		log.Printf("Unhandled message %q", msg.Subject().Subject())
	}
	return nil
}
