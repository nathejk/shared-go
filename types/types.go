package types

import (
	"crypto/md5"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type Sequence = uint64
type ID = string
type Slug = string
type Enum = string

/** Dictionary
 * Squad:  a small group of people having a particular task.
 * Patrol: a unit of six to eight Scouts or Guides forming part of a troop.
 */

type TeamID ID

func (t TeamID) New() TeamID {
	return TeamID("team-" + uuid.New().String())
}

type MemberID ID

func (ID MemberID) New() MemberID {
	return MemberID("member-" + uuid.New().String())
}

type ScanID ID
type AttachmentID ID
type LegacyID ID

func (ID LegacyID) Checksum() string {
	// PHP: return substr(md5($this->id . '**@'), -5);
	md5sum := md5.Sum([]byte(string(ID) + "**@"))
	hash := hex.EncodeToString(md5sum[:])
	return hash[len(hash)-5:]
}
func (ID LegacyID) Year() uint {
	number, err := strconv.ParseUint(string(ID[0:4]), 10, 32)
	if err != nil {
		return 0
	}
	return uint(number)
}

type UserID ID

func (id UserID) IsSlackUser() bool {
	return strings.HasPrefix(string(id), "slack-")
}

type Email string

type PingType string

const (
	PingTypeSignup          PingType = "signup"
	PingTypeMobilepayLink   PingType = "mobilepay"
	PingTypePaymentReceived PingType = "payment"
	PingTypeWelcome         PingType = "welcome"
	PingTypeTeamUpdated     PingType = "teamupdate"
)
