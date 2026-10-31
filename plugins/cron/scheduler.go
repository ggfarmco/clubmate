package main

import (
	"time"

	"clubmate/plugins/internal/automation"
)

const (
	lateGrace = time.Minute
	tick      = 30 * time.Second
)

type decision int

const (
	fire decision = iota + 1
	skipMissed
	wait
)

func decide(j *Job, now time.Time) decision {
	if !j.Enabled || now.Before(j.NextRun) {
		return wait
	}
	if now.Sub(j.NextRun) <= lateGrace || j.Missed == missedOnce {
		return fire
	}
	return skipMissed
}

func advance(j *Job, now time.Time) (keep bool) {
	next, ok := j.Schedule.Next(now)
	if ok {
		j.NextRun = next
		return true
	}
	if j.Schedule.OneShot() {
		return false
	}
	j.Enabled = false
	j.LastError = "schedule has no next run"
	return true
}

func record(j *Job, r Run) (paused bool) {
	if !automation.Record(&j.History, &j.Failures, &j.LastError, r) {
		return false
	}
	j.Enabled = false
	return true
}

func wakeAfter(jobs []*Job, now time.Time) time.Duration {
	wake := tick
	for _, j := range jobs {
		if !j.Enabled || j.NextRun.IsZero() {
			continue
		}
		wake = min(wake, max(j.NextRun.Sub(now), 0))
	}
	return wake
}
