package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const minEvery = time.Minute

var (
	errExactlyOne = errors.New("give exactly one of cron, every, at, in")
	cronParser    = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
)

type Schedule struct {
	Cron     string    `json:"cron,omitempty"`
	Timezone string    `json:"timezone,omitempty"`
	Every    string    `json:"every,omitempty"`
	At       time.Time `json:"at,omitzero"`
}

func parseSchedule(cronExpr, timezone, every, at, in string, now time.Time) (Schedule, error) {
	given := 0
	for _, v := range []string{cronExpr, every, at, in} {
		if v != "" {
			given++
		}
	}
	if given != 1 {
		return Schedule{}, errExactlyOne
	}
	loc, err := loadLocation(timezone)
	if err != nil {
		return Schedule{}, fmt.Errorf("timezone %q: %w", timezone, err)
	}
	s := Schedule{Timezone: timezone}
	switch {
	case cronExpr != "":
		if hasTZPrefix(cronExpr) {
			return Schedule{}, fmt.Errorf("cron %q: set the zone with the timezone field, not a TZ= prefix", cronExpr)
		}
		spec, err := cronParser.Parse(cronExpr)
		if err != nil {
			return Schedule{}, fmt.Errorf("cron %q: %w", cronExpr, err)
		}
		if spec.Next(now.In(loc)).IsZero() {
			return Schedule{}, fmt.Errorf("cron %q: never fires", cronExpr)
		}
		s.Cron = cronExpr
	case every != "":
		d, err := time.ParseDuration(every)
		if err != nil {
			return Schedule{}, fmt.Errorf("every %q: %w", every, err)
		}
		if d < minEvery {
			return Schedule{}, fmt.Errorf("every %q: must be at least 1m", every)
		}
		s.Every = every
	case at != "":
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return Schedule{}, fmt.Errorf("at %q: %w", at, err)
		}
		s.At = t
	default:
		d, err := time.ParseDuration(in)
		if err != nil {
			return Schedule{}, fmt.Errorf("in %q: %w", in, err)
		}
		if d <= 0 {
			return Schedule{}, fmt.Errorf("in %q: must be positive", in)
		}
		s.At = now.Round(0).Add(d)
	}
	return s, nil
}

func (s Schedule) Next(after time.Time) (time.Time, bool) {
	after = after.Round(0)
	switch {
	case s.OneShot():
		if after.Before(s.At) {
			return s.At, true
		}
		return time.Time{}, false
	case s.Every != "":
		d, err := time.ParseDuration(s.Every)
		if err != nil {
			return time.Time{}, false
		}
		return after.Add(d), true
	default:
		spec, err := cronParser.Parse(s.Cron)
		if err != nil {
			return time.Time{}, false
		}
		loc, err := loadLocation(s.Timezone)
		if err != nil {
			return time.Time{}, false
		}
		next := nextOnWall(spec, after.In(loc))
		return next, !next.IsZero()
	}
}

func (s Schedule) OneShot() bool {
	return !s.At.IsZero()
}

func (s Schedule) Describe() string {
	switch {
	case s.OneShot():
		loc, err := loadLocation(s.Timezone)
		if err != nil {
			loc = time.Local
		}
		return "once at " + s.At.In(loc).Format("2006-01-02 15:04")
	case s.Every != "":
		return "every " + s.Every
	default:
		if hh, mm, ok := dailyTime(s.Cron); ok {
			return fmt.Sprintf("every day %02d:%02d", hh, mm)
		}
		return "cron " + s.Cron
	}
}

func loadLocation(timezone string) (*time.Location, error) {
	if timezone == "" {
		return time.Local, nil
	}
	return time.LoadLocation(timezone)
}

func hasTZPrefix(expr string) bool {
	expr = strings.TrimSpace(expr)
	return strings.HasPrefix(expr, "TZ=") || strings.HasPrefix(expr, "CRON_TZ=")
}

func dailyTime(expr string) (int, int, bool) {
	fields := strings.Fields(expr)
	if len(fields) != 5 || fields[2] != "*" || fields[3] != "*" || fields[4] != "*" {
		return 0, 0, false
	}
	mm, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false
	}
	hh, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false
	}
	return hh, mm, true
}

func nextOnWall(spec cron.Schedule, after time.Time) time.Time {
	wall := spec.Next(wallClock(after))
	if wall.IsZero() {
		return time.Time{}
	}
	var first, skipped time.Time
	for _, probe := range []time.Duration{-24 * time.Hour, 0, 24 * time.Hour} {
		_, offset := wall.Add(probe).In(after.Location()).Zone()
		t := wall.Add(-time.Duration(offset) * time.Second).In(after.Location())
		switch {
		case !wallClock(t).Equal(wall):
			if start, _ := t.ZoneBounds(); start.After(after) && start.After(skipped) {
				skipped = start
			}
		case t.After(after) && (first.IsZero() || t.Before(first)):
			first = t
		}
	}
	if first.IsZero() {
		first = skipped
	}
	if first.IsZero() {
		return spec.Next(after)
	}
	return first
}

func wallClock(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}
