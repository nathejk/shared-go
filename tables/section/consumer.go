package section

import (
	"fmt"
	"log"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/mysql"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

// bodyNathejkSectionsSorted is the wire body for NATHEJK.{year}.sections.sorted.
//
// TODO: once github.com/nathejk/shared-go declares a NathejkSectionsSorted
// message type, replace this local struct with an import from that package.
// The JSON shape below is the contract; keep it stable.
type bodyNathejkSectionsSorted struct {
	ParentSectionSlug types.Slug   `json:"parentSectionSlug"`
	SortedSlugs       []types.Slug `json:"sortedSlugs"`
}

// bodyNathejkSectionMoved is the wire body for NATHEJK.{year}.section.{slug}.moved.
//
// TODO: once github.com/nathejk/shared-go declares a NathejkSectionMoved
// message type, replace this local struct with an import from that package.
type bodyNathejkSectionMoved struct {
	Slug              types.Slug `json:"slug"`
	ParentSectionSlug types.Slug `json:"parentSectionSlug"`
}

type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() []cqrs.Subject {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK.*.section.*.added"),
		cqrs.SubjectFromStr("NATHEJK.*.section.*.moved"),
		cqrs.SubjectFromStr("NATHEJK.*.section.*.deleted"),
		cqrs.SubjectFromStr("NATHEJK.*.sections.sorted"),
	}
}

func (c *consumer) HandleMessage(msg cqrs.Message) error {
	dialect := goqu.Dialect("mysql")

	switch true {
	case msg.Subject().Match("NATHEJK.*.section.*.added"):
		var body messages.NathejkSectionAdded
		if err := msg.Body(&body); err != nil {
			return err
		}
		year := msg.Subject().Parts()[1]

		// Insert a new row with sortOrder = max(sortOrder)+1 for the parent, or
		// leave existing rows alone (upsert on (year, slug) preserves
		// sortOrder because we don't list it in the UPDATE clause).
		//
		// goqu doesn't compose INSERT ... SELECT easily across MySQL dialects,
		// so we hand-write the query with parameter placeholders and let the
		// SQL executor handle quoting via fmt.Sprintf(%q). This mirrors the
		// style used by the lok consumer.
		sqlStr := fmt.Sprintf(
			`INSERT INTO section (slug, year, parentSlug, label, sortOrder)
			 SELECT %q, %q, %q, %q, COALESCE(MAX(sortOrder)+1, 0)
			 FROM section WHERE year = %q AND parentSlug = %q
			 ON DUPLICATE KEY UPDATE parentSlug = VALUES(parentSlug), label = VALUES(label)`,
			string(body.Slug), year, string(body.ParentSectionSlug), body.Label,
			year, string(body.ParentSectionSlug),
		)
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.section.*.moved"):
		var body bodyNathejkSectionMoved
		if err := msg.Body(&body); err != nil {
			return err
		}
		year := msg.Subject().Parts()[1]
		// Reparent + place at the end of the new parent's children in one
		// statement. MySQL forbids reading the table being updated directly,
		// hence the nested subquery workaround.
		sqlStr := fmt.Sprintf(
			`UPDATE section
			 SET parentSlug = %q,
			     sortOrder  = COALESCE((SELECT s FROM (SELECT MAX(sortOrder)+1 AS s FROM section WHERE year = %q AND parentSlug = %q) AS x), 0)
			 WHERE year = %q AND slug = %q`,
			string(body.ParentSectionSlug),
			year, string(body.ParentSectionSlug),
			year, string(body.Slug),
		)
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.section.*.deleted"):
		var body messages.NathejkSectionDeleted
		if err := msg.Body(&body); err != nil {
			return err
		}
		year := msg.Subject().Parts()[1]
		sqlStr, _, err := dialect.Delete("section").Where(goqu.Ex{
			"year": year,
			"slug": string(body.Slug),
		}).ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.sections.sorted"):
		var body bodyNathejkSectionsSorted
		if err := msg.Body(&body); err != nil {
			return err
		}
		year := msg.Subject().Parts()[1]
		if len(body.SortedSlugs) == 0 {
			return nil
		}
		// Rewrite sortOrder = index within the given sibling list, scoped to
		// (year, parentSlug, slug) so a stray slug can't accidentally clobber
		// another parent's row.
		for i, slug := range body.SortedSlugs {
			q := fmt.Sprintf(
				`UPDATE section SET sortOrder = %d WHERE year = %q AND parentSlug = %q AND slug = %q`,
				i, year, string(body.ParentSectionSlug), string(slug),
			)
			if err := c.w.Consume(q); err != nil {
				log.Printf("section: sort update failed for %s: %v", slug, err)
			}
		}
		return nil

	default:
		log.Printf("section: unhandled message %q", msg.Subject().Subject())
	}
	return nil
}
