package messages

import (
	"time"

	"github.com/nathejk/shared-go/types"
)

type NathejkControlGroup_DateRange struct {
	StartDate time.Time `json:"startDate"`
	EndDate   time.Time `json:"endDate"`
}

func (r *NathejkControlGroup_DateRange) In(date time.Time) bool {
	return r.StartDate.Before(date) && r.EndDate.After(date)
}

type NathejkControlGroup_Scanner struct {
	DateRange NathejkControlGroup_DateRange `json:"dateRange"`
	UserID    types.UserID                  `json:"userId"`
}

type NathejkControlGroup_Control struct {
	Name                 string                        `json:"name"`
	Scheme               string                        `json:"scheme"`
	RelativeCheckgroupID types.ControlGroupID          `json:"relativeControlGroupId"`
	DateRange            NathejkControlGroup_DateRange `json:"dateRange"`
	Minus                int                           `json:"minus"`
	Plus                 int                           `json:"plus"`
	Scanners             []NathejkControlGroup_Scanner `json:"scanners"`
}

// nathejk:user.updated
type NathejkControlGroupUpdated struct {
	ControlGroupID types.ControlGroupID          `json:"controlGroupId"`
	Name           string                        `json:"name"`
	Controls       []NathejkControlGroup_Control `json:"controls"`
}

// nathejk:user.deleted
type NathejkControlGroupDeleted struct {
	ControlGroupID types.ControlGroupID `json:"controlGroupId"`
}
