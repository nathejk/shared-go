package types

type ControlGroupID ID
type CheckgroupID ID
type CheckpointID ID
type CheckgroupScheme string

const (
	CheckgroupSchemeFixed    CheckgroupScheme = "fixed"
	CheckgroupSchemeRelative CheckgroupScheme = "relative"
	CheckgroupSchemeNone     CheckgroupScheme = "none"
)
