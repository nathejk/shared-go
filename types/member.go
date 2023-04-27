package types

type MemberStatus string

const (
	MemberStatusNone       MemberStatus = ""
	MemberStatusRegistered MemberStatus = "REGISTERED"
	MemberStatusStarted    MemberStatus = "STARTED"
)
