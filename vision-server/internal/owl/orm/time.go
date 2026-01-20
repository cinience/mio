package orm

import "time"

type Time struct {
	time.Time
}

func Now() Time {
	return Time{Time: time.Now()}
}
