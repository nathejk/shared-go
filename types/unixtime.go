package types

import (
	"log"
	"strconv"
	"time"
)

type UnixtimeString string

func (uts UnixtimeString) Time() *time.Time {
	if (uts == "") || (uts == "0") {
		return nil
	}
	intUnix, err := strconv.ParseInt(string(uts), 10, 64)
	// int64 to time.Time
	tm := time.Unix(intUnix, 0)
	//tm, err := time.Parse("1136239445", string(uts))
	if err != nil || tm.IsZero() {
		log.Print(err)
		return nil
	}
	return &tm
}

type UnixtimeInteger int64

func (uts UnixtimeInteger) Time() *time.Time {
	ts := time.Unix(int64(uts), 0)
	return &ts
}
