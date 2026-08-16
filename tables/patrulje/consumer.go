package patrulje

import (
	"fmt"
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() (subjs []cqrs.Subject) {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK:*.patrulje.*.signedup"),
		cqrs.SubjectFromStr("NATHEJK:*.patrulje.*.updated"),
		cqrs.SubjectFromStr("NATHEJK:*.patrulje.*.numberassigned"),
		cqrs.SubjectFromStr("NATHEJK:*.patrulje.*.started"),
	}
}

func (c *consumer) HandleMessage(msg cqrs.Message) error {
	//log.Printf("patrulje.go RECEIVED %q", msg.Subject().Subject())
	switch true {
	case msg.Subject().Match("NATHEJK.*.patrulje.*.signedup"):
		var body messages.NathejkTeamSignedUp
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.TeamID == "" {
			return nil
		}
		// The year comes off the signup's subject, like klan's projector does,
		// not from msg.Time().Year(): the subject carries the season the team
		// signed up for, which is the same value only until a season is opened in
		// the preceding calendar year. Everything downstream inherits this year —
		// the commands read it back off the row to build their subjects.
		sql := fmt.Sprintf("INSERT INTO patrulje SET teamId=%q, year=%q, contactName=%q, contactPhone=%q, contactEmail=%q ON DUPLICATE KEY UPDATE contactName=VALUES(contactName), contactPhone=VALUES(contactPhone), contactEmail=VALUES(contactEmail)", body.TeamID, msg.Subject().Parts()[1], body.Name, body.Phone, body.Email)
		if err := c.w.Consume(sql); err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}
	case msg.Subject().Match("NATHEJK.*.patrulje.*.updated"):
		var body messages.NathejkTeamUpdated
		if err := msg.Body(&body); err != nil {
			return err
		}
		msg.Subject().Parts()
		query := "UPDATE patrulje SET name=%q, groupName=%q, korps=%q, liga=%q, contactName=%q, contactPhone=%q, contactEmail=%q, contactRole=%q WHERE teamId=%q"
		args := []any{body.Name, body.GroupName, body.Korps, body.AdvspejdNumber, body.ContactName, body.ContactPhone, body.ContactEmail, substr(body.ContactRole, 0, 90), body.TeamID}

		err := c.w.Consume(fmt.Sprintf(query, args...))
		if err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}

	case msg.Subject().Match("NATHEJK.*.patrulje.*.numberassigned"):
		var body messages.NathejkPatrolNumberAssigned
		if err := msg.Body(&body); err != nil {
			return err
		}
		query := "UPDATE patrulje SET teamNumber=%q WHERE teamId=%q"
		args := []any{body.TeamNumber, body.TeamID}

		if err := c.w.Consume(fmt.Sprintf(query, args...)); err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}

	case msg.Subject().Match("NATHEJK.*.patrulje.*.started"):
		var body messages.NathejkTeamStarted
		if err := msg.Body(&body); err != nil {
			return err
		}
		query := "UPDATE patrulje SET signupStatus=%q, memberCount=%d WHERE teamId=%q"
		args := []any{types.SignupStatusStarted, len(body.Members), body.TeamID}

		if err := c.w.Consume(fmt.Sprintf(query, args...)); err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}
	default:
		log.Printf("Unhandled message %q", msg.Subject().Subject())

	}
	return nil
}
func substr(input string, start int, length int) string {
	asRunes := []rune(input)

	if start >= len(asRunes) {
		return ""
	}

	if start+length > len(asRunes) {
		length = len(asRunes) - start
	}

	return string(asRunes[start : start+length])
}
