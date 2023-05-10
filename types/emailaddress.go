package types

import "regexp"

// Declare a regular expression for sanity checking the format of email addresses
// pattern is taken from https://html.spec.whatwg.org/#valid-e-mail-address.
var (
	emailRX = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+\\/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
)

type EmailAddress string

func (m EmailAddress) Valid() bool {
	return emailRX.MatchString(m)
}
