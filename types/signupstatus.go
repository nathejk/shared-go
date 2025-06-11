package types

type SignupStatus string

const (
	SignupStatusNone    SignupStatus = ""
	SignupStatusNew     SignupStatus = "NEW"
	SignupStatusOnHold  SignupStatus = "HOLD"
	SignupStatusPay     SignupStatus = "PAY"
	SignupStatusPaid    SignupStatus = "PAID"
	SignupStatusStarted SignupStatus = "STARTED"
	SignupStatusOut     SignupStatus = "OUT"
)

type TeamType string
type TeamTypeList []TeamType

const (
	TeamTypePatrulje TeamType = "patrulje"
	TeamTypeKlan     TeamType = "klan"
	TeamTypeStaff    TeamType = "staff"
	TeamTypeBadut    TeamType = "gøgler"
	TeamTypePatrol   TeamType = "patrulje"
	TeamTypeSquad    TeamType = "klan"

// TeamTypes        TeamTypeList = TeamTypeList{TeamTypePatrulje, TeamTypeKlan}
)

var TeamTypes = TeamTypeList{TeamTypePatrulje, TeamTypeKlan, TeamTypeStaff, TeamTypeBadut}

// type TeamTypes []TeamType
func (l *TeamTypeList) Exists(t TeamType) bool {
	for _, i := range TeamTypes {
		if t == i {
			return true
		}
	}
	return false
}
