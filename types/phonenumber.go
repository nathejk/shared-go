package types

import (
	"regexp"
	"strings"
)

type PhoneNumber string

func (v PhoneNumber) IsValid() bool {
	return len(v.Normalize()) == 8
}
func (v PhoneNumber) Normalize() string {
	re := regexp.MustCompile("[0-9]+")
	return strings.Join(re.FindAllString(string(v), -1), "")
}
