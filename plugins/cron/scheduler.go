package main

import "time"

const (
	lateGrace   = time.Minute
	maxHistory  = 20
	maxFailures = 3
	tick        = 30 * time.Second
	maxResult   = 200
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
	j.History = append(j.History, r)
	if extra := len(j.History) - maxHistory; extra > 0 {
		j.History = j.History[extra:]
	}
	switch r.Status {
	case statusOK:
		j.Failures = 0
		j.LastError = ""
	case statusError:
		j.Failures++
		j.LastError = r.Reason
		if j.Failures >= maxFailures {
			j.Enabled = false
			return true
		}
	}
	return false
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
