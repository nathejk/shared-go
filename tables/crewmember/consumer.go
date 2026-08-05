package crewmember

import (
	"encoding/json"
	"log"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/mysql"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
)

type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() []cqrs.Subject {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK.*.crewmember.*.registered"),
		cqrs.SubjectFromStr("NATHEJK.*.crewmember.*.updated"),
		cqrs.SubjectFromStr("NATHEJK.*.crewmember.*.deleted"),
		cqrs.SubjectFromStr("NATHEJK.*.crewmember.*.section.assigned"),
		// Crew signups flow through the shared signup pipeline as
		// NATHEJK.<year>.crew.<teamId>.signedup. Projecting them here (rather
		// than into personnel) is what makes a crew signup a crew member.
		cqrs.SubjectFromStr("NATHEJK.*.crew.*.signedup"),
	}
}

func (c *consumer) HandleMessage(msg cqrs.Message) error {
	dialect := goqu.Dialect("mysql")
	parts := msg.Subject().Parts()

	switch true {
	case msg.Subject().Match("NATHEJK.*.crew.*.signedup"):
		var body messages.NathejkTeamSignedUp
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.TeamID == "" {
			return nil
		}
		// A crew signup mints the crew member. userId == teamId so the crew
		// page (and its order) key off the same id the signup handed out.
		year := parts[1]
		insert := goqu.Record{
			"userId":      string(body.TeamID),
			"year":        year,
			"name":        body.Name,
			"phone":       string(body.Phone),
			"email":       string(body.Email),
			"additionals": "{}",
			"deleted":     0,
		}
		update := goqu.Record{
			"name":    goqu.L("VALUES(name)"),
			"phone":   goqu.L("VALUES(phone)"),
			"email":   goqu.L("VALUES(email)"),
			"deleted": 0,
		}
		sqlStr, _, err := dialect.Insert("crewmember").Rows(insert).OnConflict(goqu.DoUpdate("userId", update)).ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.crewmember.*.registered"):
		var body messages.NathejkCrewMemberRegistered
		if err := msg.Body(&body); err != nil {
			return err
		}
		year := parts[1]
		insert := goqu.Record{
			"userId":      string(body.UserID),
			"year":        year,
			"name":        body.Name,
			"phone":       string(body.Phone),
			"email":       string(body.Email),
			"additionals": "{}",
			"deleted":     0,
		}
		update := goqu.Record{
			"year":    goqu.L("VALUES(year)"),
			"name":    goqu.L("VALUES(name)"),
			"phone":   goqu.L("VALUES(phone)"),
			"email":   goqu.L("VALUES(email)"),
			"deleted": 0,
		}
		sqlStr, _, err := dialect.Insert("crewmember").Rows(insert).OnConflict(goqu.DoUpdate("userId", update)).ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.crewmember.*.updated"):
		var body messages.NathejkCrewMemberUpdated
		if err := msg.Body(&body); err != nil {
			return err
		}
		year := parts[1]
		additionals := "{}"
		if body.Additionals != nil {
			b, _ := json.Marshal(body.Additionals)
			additionals = string(b)
		}
		// Insert-or-update so an "updated" event received before "registered"
		// (out-of-order replay) still produces a row.
		insert := goqu.Record{
			"userId":      string(body.UserID),
			"year":        year,
			"name":        body.Name,
			"phone":       string(body.Phone),
			"email":       string(body.Email),
			"medlemNr":    body.MedlemNr,
			"groupName":   body.Group,
			"corps":       string(body.Corps),
			"diet":        body.Diet,
			"additionals": additionals,
		}
		update := goqu.Record{
			"name":        goqu.L("VALUES(name)"),
			"phone":       goqu.L("VALUES(phone)"),
			"email":       goqu.L("VALUES(email)"),
			"medlemNr":    goqu.L("VALUES(medlemNr)"),
			"groupName":   goqu.L("VALUES(groupName)"),
			"corps":       goqu.L("VALUES(corps)"),
			"diet":        goqu.L("VALUES(diet)"),
			"additionals": goqu.L("VALUES(additionals)"),
		}
		sqlStr, _, err := dialect.Insert("crewmember").Rows(insert).OnConflict(goqu.DoUpdate("userId", update)).ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.crewmember.*.deleted"):
		var body messages.NathejkCrewMemberDeleted
		if err := msg.Body(&body); err != nil {
			return err
		}
		// Soft-delete so subsequent events for this user don't accidentally
		// resurrect through the upsert path used by "updated".
		sqlStr, _, err := dialect.Update("crewmember").
			Set(goqu.Record{"deleted": 1, "sectionSlug": ""}).
			Where(goqu.Ex{"userId": string(body.UserID)}).
			ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.crewmember.*.section.assigned"):
		var body messages.NathejkCrewMemberSectionAssigned
		if err := msg.Body(&body); err != nil {
			return err
		}
		// Overwriting sectionSlug is exactly the "silent unassignment" the
		// domain calls for: a crew member is always in at most one section.
		year := parts[1]
		insert := goqu.Record{
			"userId":      string(body.UserID),
			"year":        year,
			"additionals": "{}",
			"sectionSlug": string(body.SectionSlug),
		}
		update := goqu.Record{
			"sectionSlug": goqu.L("VALUES(sectionSlug)"),
		}
		sqlStr, _, err := dialect.Insert("crewmember").Rows(insert).OnConflict(goqu.DoUpdate("userId", update)).ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	default:
		log.Printf("crewmember: unhandled message %q", msg.Subject().Subject())
	}
	return nil
}
