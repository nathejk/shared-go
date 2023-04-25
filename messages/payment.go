package messages

import (
	"time"

	"github.com/nathejk/shared-go/types"
)

type NathejkPaymentReceived struct {
	TeamID    types.TeamID   `json:"teamId,omitempty"`
	MemberID  types.MemberID `json:"memberId,omitempty"`
	Amount    float32        `json:"amount"`
	Currency  string         `json:"currency"`
	Timestamp time.Time      `json:"timestamp"`
}
