package types

import "time"

type Date string

func (d Date) ToTime() (time.Time, error) {
	return time.Parse("2006-01-02", string(d))
}

func DateFromTime(t time.Time) Date {
	return Date(t.Format("2006-01-02"))
}
