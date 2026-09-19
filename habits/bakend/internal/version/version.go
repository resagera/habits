package version

import (
	"time"
)

var (
	commit = "dev-917"
	date   = "2025-11-14T13:16:00"
)

type Version struct {
	Commit string
	Date   string
}

func (v Version) SemVer() string {
	d, _ := time.Parse("2006-01-02T15:04:05", v.Date)
	return d.Format("2006.01.02.150405") + "+" + v.Commit
}

func Get() Version {
	return Version{
		Commit: commit,
		Date:   date,
	}
}
