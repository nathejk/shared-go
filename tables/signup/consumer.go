package signup

import (
	"fmt"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
)

type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() []cqrs.Subject {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK:*.*.*.signedup"),
		cqrs.SubjectFromStr("NATHEJK:*.*.*.mail.validate.sent"),
		cqrs.SubjectFromStr("NATHEJK:*.*.*.sms.validate.sent"),
		cqrs.SubjectFromStr("NATHEJK:*.*.*.emailaddress.verified"),
		cqrs.SubjectFromStr("NATHEJK:*.*.*.phonenumber.verified"),
	}
}

func (c *consumer) HandleMessage(msg cqrs.Message) error {
	switch true {
	case msg.Subject().Match("NATHEJK.*.*.*.signedup"):
		//case "NATHEJK.year.created":
		var body messages.NathejkTeamSignedUp
		if err := msg.Body(&body); err != nil {
			return err
		}
		sql := "INSERT INTO signup SET teamId=%q, year=%q, teamType=%q, name=%q, emailPending=%q, phonePending=%q, pincode=%q, createdAt=%q ON DUPLICATE KEY UPDATE name=VALUES(name), emailPending=VALUES(emailPending), phonePending=VALUES(phonePending), pincode=VALUES(pincode)"
		args := []any{
			body.TeamID,
			msg.Subject().Parts()[1],
			msg.Subject().Parts()[2],
			body.Name,
			body.Email,
			body.Phone,
			body.Pincode,
			msg.Time(),
		}
		if err := c.w.Consume(fmt.Sprintf(sql, args...)); err != nil {
			return err
		}

	case msg.Subject().Match("NATHEJK.*.*.*.mail.validate.sent"):
		var body messages.NathejkMailSent
		if err := msg.Body(&body); err != nil {
			return err
		}
		sql := "UPDATE signup SET secret=%q WHERE teamId=%q"
		args := []any{
			body.Secret,
			body.TeamID,
		}
		if err := c.w.Consume(fmt.Sprintf(sql, args...)); err != nil {
			return err
		}

	case msg.Subject().Match("NATHEJK.*.*.*.sms.validate.sent"):
		var body messages.NathejkSmsSent
		if err := msg.Body(&body); err != nil {
			return err
		}
		sql := "UPDATE signup SET pincode=%q WHERE teamId=%q"
		args := []any{
			body.Secret,
			body.TeamID,
		}
		if err := c.w.Consume(fmt.Sprintf(sql, args...)); err != nil {
			return err
		}

	case msg.Subject().Match("NATHEJK.*.*.*.emailaddress.verified"):
		var body messages.NathejkSignupEmailVerified
		if err := msg.Body(&body); err != nil {
			return err
		}
		sql := "UPDATE signup SET email=emailPending WHERE teamId=%q"
		args := []any{
			body.TeamID,
		}
		if err := c.w.Consume(fmt.Sprintf(sql, args...)); err != nil {
			return err
		}

	case msg.Subject().Match("NATHEJK.*.*.*.phonenumber.verified"):
		var body messages.NathejkSignupPhoneVerified
		if err := msg.Body(&body); err != nil {
			return err
		}
		sql := "UPDATE signup SET phone=phonePending WHERE teamId=%q"
		args := []any{
			body.TeamID,
		}
		if err := c.w.Consume(fmt.Sprintf(sql, args...)); err != nil {
			return err
		}
	}
	return nil
}
