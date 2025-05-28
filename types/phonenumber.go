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
func (v PhoneNumber) CountryCode() string {
	if len(v.Normalize()) == 8 {
		return "45"
	}
	return ""
}

func (v PhoneNumber) InternationalNumber() string {
	return v.CountryCode() + v.Normalize()
}
