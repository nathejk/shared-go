package types

type SignupStatus string

const (
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
	TeamTypePatrol   TeamType = "patrulje"
	TeamTypeSquad    TeamType = "klan"

// TeamTypes        TeamTypeList = TeamTypeList{TeamTypePatrulje, TeamTypeKlan}
)

var TeamTypes = TeamTypeList{TeamTypePatrulje, TeamTypeKlan}

// type TeamTypes []TeamType
func (l *TeamTypeList) Exists(t TeamType) bool {
	for _, i := range TeamTypes {
		if t == i {
			return true
		}
	}
	return false
}
