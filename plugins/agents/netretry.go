package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"clubmate/plugins/agents/acp"
)

const (
	netRetryAfter    = time.Minute
	netRetryAfterEnv = "CLUBMATE_AGENTS_NET_RETRY_AFTER"
	netRetryMax      = 8
	netRetryText     = "[Clubmate] Your turn ended with a network error (%s). Continue the task from where you stopped."
	netRetryNotice   = "Network error; retrying in %s"
	netRetryHint     = "attempt %d of %d: %s"
	netGiveUpNotice  = "Network error; stopped retrying"
	netGiveUpHint    = "send a message to continue"
)

var netErrors = []string{"No response from API", "Connection refused", "ECONNREFUSED"}

func networkError(err error) (string, bool) {
	rpcErr, ok := errors.AsType[*acp.Error](err)
	if !ok || rpcErr.Code != acp.CodeInternal {
		return "", false
	}
	for _, m := range netErrors {
		if strings.Contains(rpcErr.Message, m) {
			return firstLine(rpcErr.Message), true
		}
	}
	return "", false
}

func (a *app) retryNetwork(s *session, err error) {
	reason, ok := networkError(err)
	rec := s.snapshot()
	w := &a.watchdog
	if !ok || !(a.effectiveAFK(rec) || a.orchestrated(rec)) {
		a.resetNetRetry(rec.ID)
		return
	}
	w.mu.Lock()
	attempt := w.retries[rec.ID] + 1
	if attempt > netRetryMax {
		delete(w.retries, rec.ID)
		w.mu.Unlock()
		s.notice(netGiveUpNotice, netGiveUpHint)
		return
	}
	w.retries[rec.ID] = attempt
	delay := netRetryDelay(attempt)
	if t := w.retryTimers[rec.ID]; t != nil {
		t.Stop()
	}
	w.retryTimers[rec.ID] = time.AfterFunc(delay, func() { a.fireNetRetry(s, reason) })
	w.mu.Unlock()
	s.notice(fmt.Sprintf(netRetryNotice, delay.Round(time.Second)), fmt.Sprintf(netRetryHint, attempt, netRetryMax, reason))
}

func (a *app) fireNetRetry(s *session, reason string) {
	w := &a.watchdog
	w.mu.Lock()
	delete(w.retryTimers, s.snapshot().ID)
	w.mu.Unlock()
	select {
	case <-a.quit:
		return
	default:
	}
	a.submitIdle(s, promptArgs{Text: fmt.Sprintf(netRetryText, reason)})
}

func (a *app) resetNetRetry(id string) {
	w := &a.watchdog
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.retries, id)
	if t := w.retryTimers[id]; t != nil {
		t.Stop()
		delete(w.retryTimers, id)
	}
}

func netRetryDelay(attempt int) time.Duration {
	base := netRetryAfter
	if v := os.Getenv(netRetryAfterEnv); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents:", netRetryAfterEnv+":", err)
		} else {
			base = d
		}
	}
	return min(base<<(attempt-1), 10*base)
}
