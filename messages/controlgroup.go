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

type NathejkCheckpoint_Scanner struct {
	UserID    types.UserID    `json:"userId"`
	TimeRange types.TimeRange `json:"timeRange"`
}
type NathejkCheckgroup_Checkpoint struct {
	CheckpointID         types.CheckpointID          `json:"checkpointId"`
	Name                 string                      `json:"name"`
	Address              string                      `json:"address,omitempty"`
	Remark               string                      `json:"remark,omitempty"`
	FixedTimeRange       *types.TimeRange            `json:"dateRange,omitempty"`
	RelativeTimeDuration *time.Duration              `json:"relativeTimeDuration,omitempty"`
	Position             *types.Coordinate           `json:"position,omitempty"`
	Scanners             []NathejkCheckpoint_Scanner `json:"scanners"`
}

type NathejkCheckpointScannerAdded struct {
	UserID       types.UserID       `json:"userId"`
	CheckpointID types.CheckpointID `json:"checkpointId"`
	TimeRange    types.TimeRange    `json:"timeRange"`
}
type NathejkCheckpointScannerRemoved struct {
	UserID       types.UserID       `json:"userId"`
	CheckpointID types.CheckpointID `json:"checkpointId"`
}
type NathejkCheckpointUpdated struct {
	CheckpointID         types.CheckpointID  `json:"checkpointId"`
	CheckgroupID         *types.CheckgroupID `json:"checkgroupId,omitempty"`
	Name                 *string             `json:"name,omitempty"`
	Address              *string             `json:"address,omitempty"`
	Remark               *string             `json:"remark,omitempty"`
	FixedTimeRange       *types.TimeRange    `json:"dateRange,omitempty"`
	RelativeTimeDuration *time.Duration      `json:"relativeTimeDuration,omitempty"`
	Position             *types.Coordinate   `json:"position,omitempty"`
}

// nathejk:user.updated
type NathejkCheckgroupUpdated struct {
	CheckgroupID         types.CheckgroupID      `json:"checkgroupId"`
	Name                 *string                 `json:"name"`
	ShowOnMap            *bool                   `json:"showOnMap"`
	Mandatory            *bool                   `json:"mandatory"`
	Scheme               *types.CheckgroupScheme `json:"scheme"`
	RelativeCheckgroupID *types.CheckgroupID     `json:"relativeCheckgroupID,omitempty"`
}
type NathejkCheckgroupSorted struct {
	SortedCheckgroupIDs []types.CheckgroupID `json:"sortedCheckgroupIds"`
}
type NathejkCheckpointScansAccepted struct {
	CheckpointID types.CheckpointID `json:"checkpointId"`
	ScanIDs      []types.ScanID     `json:"scanIds"`
}
type NathejkCheckpointScansRejected struct {
	CheckpointID types.CheckpointID `json:"checkpointId"`
	ScanIDs      []types.ScanID     `json:"scanIds"`
}
type NathejkCheckgroupDeleted struct {
	CheckgroupID types.CheckgroupID `json:"checkgroupId"`
}
type NathejkCheckpointDeleted struct {
	CheckpointID types.CheckpointID `json:"checkpointId"`
}

type NathejkControlGroupUpdated struct {
	ControlGroupID types.ControlGroupID          `json:"controlGroupId"`
	Name           string                        `json:"name"`
	Controls       []NathejkControlGroup_Control `json:"controls"`
}

// nathejk:user.deleted
type NathejkControlGroupDeleted struct {
	ControlGroupID types.ControlGroupID `json:"controlGroupId"`
}
