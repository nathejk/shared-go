package section

import (
	"context"
	"errors"
	"fmt"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

// Commands is the write-side interface for sections.
type Commands interface {
	Add(ctx context.Context, year types.YearSlug, slug types.Slug, parent types.Slug, label string) error
	Rename(ctx context.Context, year types.YearSlug, slug types.Slug, label string) error
	Move(ctx context.Context, year types.YearSlug, slug types.Slug, newParent types.Slug) error
	Sort(ctx context.Context, year types.YearSlug, parent types.Slug, sortedSlugs []types.Slug) error
	Delete(ctx context.Context, year types.YearSlug, slug types.Slug) error
	CopyFromYear(ctx context.Context, source, dest types.YearSlug) (int, error)
}

type commander struct {
	p cqrs.Publisher
	q Queries
}

// Add publishes a NathejkSectionAdded event for the given section. Section
// slug must be a valid types.Slug and unique per (year, slug). If a parent is
// specified, the parent must already exist in the same year.
func (c commander) Add(ctx context.Context, year types.YearSlug, slug types.Slug, parent types.Slug, label string) error {
	if !slug.Valid() {
		return fmt.Errorf("invalid section slug %q", slug)
	}
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if label == "" {
		return errors.New("section label is required")
	}
	if parent != "" {
		if _, err := c.q.GetBySlug(ctx, year, parent); err != nil {
			return fmt.Errorf("parent section %q not found in year %q: %w", parent, year, err)
		}
	}
	if existing, _ := c.q.GetBySlug(ctx, year, slug); existing != nil {
		return fmt.Errorf("section %q already exists in year %q", slug, year)
	}

	body := messages.NathejkSectionAdded{
		Slug:              slug,
		ParentSectionSlug: parent,
		Label:             label,
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.section.%s.added", year, slug),
	))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// Rename publishes a NathejkSectionAdded event for an existing section with a
// new label. shared-go does not (yet) define a dedicated SectionRenamed
// event, so we reuse Added — the section consumer treats it as an upsert on
// (year, slug). Parent stays unchanged (no SectionMoved event either).
func (c commander) Rename(ctx context.Context, year types.YearSlug, slug types.Slug, label string) error {
	if label == "" {
		return errors.New("section label is required")
	}
	existing, err := c.q.GetBySlug(ctx, year, slug)
	if err != nil {
		return err
	}
	if existing.Label == label {
		return nil
	}

	body := messages.NathejkSectionAdded{
		Slug:              slug,
		ParentSectionSlug: existing.ParentSlug,
		Label:             label,
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.section.%s.added", year, slug),
	))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// Sort publishes a `sections.sorted` event with the given parent's children
// in the requested order. Validation is intentionally loose: the consumer's
// WHERE clause enforces (year, parentSlug, slug) so unknown or mis-parented
// slugs are silent no-ops on the read model, not corruption. This lets
// Sort be safely POSTed immediately after Move without racing the read
// model's application of the Move event.
func (c commander) Sort(ctx context.Context, year types.YearSlug, parent types.Slug, sortedSlugs []types.Slug) error {
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if len(sortedSlugs) == 0 {
		return errors.New("sortedSlugs must contain at least one slug")
	}

	body := bodyNathejkSectionsSorted{
		ParentSectionSlug: parent,
		SortedSlugs:       sortedSlugs,
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.sections.sorted", year),
	))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// Move publishes a `section.{slug}.moved` event that reparents an existing
// section. Refuses cycles (moving a section under its own descendant) and
// no-ops when new parent == current parent.
func (c commander) Move(ctx context.Context, year types.YearSlug, slug types.Slug, newParent types.Slug) error {
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if slug == "" {
		return errors.New("slug is required")
	}
	if slug == newParent {
		return errors.New("section cannot be its own parent")
	}
	existing, err := c.q.GetBySlug(ctx, year, slug)
	if err != nil {
		return err
	}
	if existing.ParentSlug == newParent {
		return nil
	}
	if newParent != "" {
		// New parent must exist and must not be a descendant of the section
		// being moved (else we'd create a cycle). Walk up the ancestry.
		all, err := c.q.GetAll(ctx, Filter{YearSlug: year})
		if err != nil {
			return err
		}
		bySlug := map[types.Slug]Section{}
		for _, s := range all {
			bySlug[s.Slug] = s
		}
		cur, ok := bySlug[newParent]
		if !ok {
			return fmt.Errorf("new parent %q not found in year %q", newParent, year)
		}
		for cur.ParentSlug != "" {
			if cur.ParentSlug == slug {
				return fmt.Errorf("cannot move %q under its own descendant %q", slug, newParent)
			}
			next, ok := bySlug[cur.ParentSlug]
			if !ok {
				break // dangling parent — give up walking
			}
			cur = next
		}
	}

	body := bodyNathejkSectionMoved{
		Slug:              slug,
		ParentSectionSlug: newParent,
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.section.%s.moved", year, slug),
	))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// Delete publishes a NathejkSectionDeleted event. Refuses when the section
// has direct child sections. Member-side checks (crew members still assigned)
// must be performed by the caller since the section package has no visibility
// into the crewmember read model.
func (c commander) Delete(ctx context.Context, year types.YearSlug, slug types.Slug) error {
	if _, err := c.q.GetBySlug(ctx, year, slug); err != nil {
		return err
	}
	n, err := c.q.CountChildren(ctx, year, slug)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("cannot delete section %q: it still has %d child section(s)", slug, n)
	}

	body := messages.NathejkSectionDeleted{Slug: slug}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
		fmt.Sprintf("NATHEJK.%s.section.%s.deleted", year, slug),
	))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// CopyFromYear republishes NathejkSectionAdded events for every section
// present in `source` under the `dest` year subject. Slugs and structure are
// preserved so that any existing NathejkCrewMemberSectionAssigned events keep
// resolving to the (re)created sections. Returns the number of sections
// copied.
func (c commander) CopyFromYear(ctx context.Context, source, dest types.YearSlug) (int, error) {
	if source == dest {
		return 0, errors.New("source and destination year must differ")
	}
	if !dest.Valid() {
		return 0, fmt.Errorf("invalid destination year %q", dest)
	}
	existing, err := c.q.GetAll(ctx, Filter{YearSlug: dest})
	if err != nil {
		return 0, err
	}
	if len(existing) > 0 {
		return 0, fmt.Errorf("destination year %q already has %d section(s); refusing to copy", dest, len(existing))
	}
	src, err := c.q.GetAll(ctx, Filter{YearSlug: source})
	if err != nil {
		return 0, err
	}
	if len(src) == 0 {
		return 0, fmt.Errorf("source year %q has no sections to copy", source)
	}

	// Emit parents before children by iterating breadth-first. The section
	// consumer is idempotent and does not enforce parent existence, but we
	// still order for cleanliness and to avoid transient "orphan" reads.
	pending := src
	emitted := map[types.Slug]bool{"": true}
	count := 0
	for len(pending) > 0 {
		progress := false
		remaining := pending[:0]
		for _, s := range pending {
			if !emitted[s.ParentSlug] {
				remaining = append(remaining, s)
				continue
			}
			body := messages.NathejkSectionAdded{
				Slug:              s.Slug,
				ParentSectionSlug: s.ParentSlug,
				Label:             s.Label,
			}
			msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
				fmt.Sprintf("NATHEJK.%s.section.%s.added", dest, s.Slug),
			))
			msg.SetBody(&body)
			if err := c.p.Publish(msg); err != nil {
				return count, err
			}
			emitted[s.Slug] = true
			count++
			progress = true
		}
		if !progress {
			// remaining sections have a parent that isn't in the source set
			// (data corruption); emit them anyway so we don't hang.
			for _, s := range remaining {
				body := messages.NathejkSectionAdded{
					Slug:              s.Slug,
					ParentSectionSlug: s.ParentSlug,
					Label:             s.Label,
				}
				msg := c.p.MessageFunc()(cqrs.SubjectFromStr(
					fmt.Sprintf("NATHEJK.%s.section.%s.added", dest, s.Slug),
				))
				msg.SetBody(&body)
				if err := c.p.Publish(msg); err != nil {
					return count, err
				}
				count++
			}
			break
		}
		pending = remaining
	}
	return count, nil
}
