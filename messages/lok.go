package messages

import "github.com/nathejk/shared-go/types"

// nathejk:*.lok.*.updated
type NathejkLokUpdated struct {
	LokID     types.LokID    `json:"lokId"`
	Name      string         `json:"name"`
	SortOrder int            `json:"sortOrder"`
	UserIDs   []types.UserID `json:"userIds"`
	TeamIDs   []types.TeamID `json:"teamIds"`
}

// nathejk:*.lok.*.deleted
type NathejkLokDeleted struct {
	LokID types.LokID `json:"lokId"`
}
