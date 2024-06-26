package messages

import (
	"time"

	"github.com/nathejk/shared-go/types"
)

// nathejk:sms.sent
type NathejkSmsSent struct {
	PingType types.PingType    `json:"pingType"`
	TeamID   types.TeamID      `json:"teamId,omitempty"`
	Phone    types.PhoneNumber `json:"phone"`
	Text     string            `json:"text"`
	Secret   string            `json:"secret,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// nathejk:mail.sent
type NathejkMailSent struct {
	PingType  types.PingType     `json:"pingType"`
	TeamID    types.TeamID       `json:"teamId,omitempty"`
	MemberID  types.MemberID     `json:"memberId,omitempty"`
	MessageID string             `json:"messageId"`
	Recipient types.EmailAddress `json:"recipient"`
	Subject   string             `json:"subject"`
	Timestamp time.Time          `json:"timestamp"`
	Secret    string             `json:"secret,omitempty"`
	Error     string             `json:"error,omitempty"`
}
