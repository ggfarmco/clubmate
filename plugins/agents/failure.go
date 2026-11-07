package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"clubmate/plugins/agents/acp"
)

const (
	failureAccess    = "access_denied"
	failureAuth      = "auth_required"
	failureRequest   = "bad_request"
	failureContext   = "context_exhausted"
	failureInternal  = "internal_error"
	failureNetwork   = "network_error"
	failureOverload  = "overloaded"
	failureProvider  = "provider_error"
	failureQuota     = "quota_exhausted"
	failureRateLimit = "rate_limited"
	failureTransport = "transport_lost"
	failureShutdown  = "worker_shutdown"

	internalErrorPrefix = "Internal error: "
	newSessionHint      = "Start a new session or hand this one off to continue."

	recoverSignIn   = "signin"
	recoverRetry    = "retry"
	recoverContinue = "continue"
	recoverReopen   = "reopen"
)

type (
	retryMode int

	turnFailure struct {
		kind       string
		title      string
		detail     string
		retry      retryMode
		newSession bool
	}

	textKind struct {
		text string
		kind string
	}

	failureAction struct {
		ID      string            `json:"id"`
		Label   string            `json:"label"`
		Icon    string            `json:"icon"`
		Command string            `json:"command,omitempty"`
		Args    map[string]string `json:"args,omitempty"`
		action  string
	}
)

const (
	retryInTurn retryMode = iota + 1
	retryBackoff
)

var (
	exitStatusRe = regexp.MustCompile(`exit status (-?\d+)`)
	acpErrorRe   = regexp.MustCompile(`^acp error -?\d+: `)
	stuckToolsRe = regexp.MustCompile(`without returning results for tool calls: (.+)$`)

	textKinds = []textKind{
		{text: "No response from API", kind: failureNetwork},
		{text: "Connection refused", kind: failureNetwork},
		{text: "ECONNREFUSED", kind: failureNetwork},
		{text: "Can't reach the API server", kind: failureNetwork},
		{text: "No internet route", kind: failureNetwork},
		{text: "Unable to connect to API (", kind: failureNetwork},
		{text: "Repeated 529 Overloaded errors", kind: failureOverload},
	}

	usageLimitPrefixes = []string{
		"You've hit your",
		"You've reached your",
		"You're out of usage credits",
		"Your org is out of usage · add funds to continue",
		"Your org is out of usage · contact your admin",
		"Your seat type doesn't include usage credits",
		"Your seat type doesn't include usage",
		"Your usage allocation has been disabled by your admin",
		"Your group's usage limit is set to $0",
		"Fable 5 requires usage credits",
		"You're out of extra usage",
		"Your seat type doesn't include extra usage",
	}

	errorKinds = map[string]string{
		"authentication_failed":  failureAuth,
		"oauth_org_not_allowed":  failureAuth,
		"billing_error":          failureQuota,
		"account_on_hold":        failureQuota,
		"verification_required":  failureAccess,
		"cloud_credential_error": failureAccess,
		"rate_limit":             failureRateLimit,
		"overloaded":             failureOverload,
		"invalid_request":        failureRequest,
		"model_not_found":        failureRequest,
		"max_output_tokens":      failureContext,
		"server_error":           failureProvider,
		"unknown":                failureProvider,
		"incomplete_tool_call":   failureInternal,
		"no_result":              failureInternal,
		"transport_lost":         failureTransport,
		"worker_shutdown":        failureShutdown,
	}

	failurePolicies = map[string]turnFailure{
		failureAccess:    {title: "Claude could not access the account or provider credentials."},
		failureAuth:      {title: "Sign in to continue using Claude."},
		failureRequest:   {title: "Claude could not process this request."},
		failureContext:   {title: "This Claude turn reached its configured limit.", newSession: true},
		failureInternal:  {title: "Claude Agent encountered an internal error.", retry: retryInTurn},
		failureNetwork:   {title: "Network error.", retry: retryBackoff},
		failureOverload:  {title: "Claude is temporarily overloaded.", retry: retryBackoff},
		failureProvider:  {title: "The model provider reported an error."},
		failureQuota:     {title: "The Claude account has no available quota."},
		failureRateLimit: {title: "Claude is temporarily rate limited.", retry: retryBackoff},
		failureTransport: {title: "The connection to Claude was lost.", newSession: true},
		failureShutdown:  {title: "The Claude worker is shutting down.", newSession: true},
	}
)

func classifyFailure(err error) (turnFailure, bool) {
	rpcErr, ok := errors.AsType[*acp.Error](err)
	if !ok || rpcErr.Code != acp.CodeInternal {
		return turnFailure{}, false
	}
	detail := firstLine(rpcErr.Message)
	text := strings.TrimPrefix(detail, internalErrorPrefix)
	if slices.ContainsFunc(usageLimitPrefixes, func(p string) bool { return strings.HasPrefix(text, p) }) {
		return turnFailure{kind: failureQuota, title: text, detail: detail}, true
	}
	if i := slices.IndexFunc(textKinds, func(k textKind) bool { return strings.Contains(rpcErr.Message, k.text) }); i >= 0 {
		return policyFailure(textKinds[i].kind, detail), true
	}
	var data acp.ErrorData
	if json.Unmarshal(rpcErr.Data, &data) == nil && data.ErrorKind != "" {
		return policyFailure(cmp.Or(errorKinds[data.ErrorKind], failureProvider), detail), true
	}
	return turnFailure{}, false
}

func (f turnFailure) label() string {
	return strings.TrimSuffix(f.title, ".")
}

func (f turnFailure) description() string {
	if f.newSession {
		return f.detail + " — " + newSessionHint
	}
	return f.detail
}

func failureStatus(err error) string {
	if f, ok := classifyFailure(err); ok {
		return f.title
	}
	return err.Error()
}

func policyFailure(kind, detail string) turnFailure {
	f := failurePolicies[kind]
	f.kind, f.detail = kind, detail
	return f
}

func failureWord(text string) string {
	if m := exitStatusRe.FindStringSubmatch(text); m != nil {
		return "exit " + m[1]
	}
	return ""
}

func failureOf(rec record) sessionFailure {
	word := failureWord(rec.Error)
	return sessionFailure{
		Word:  cmp.Or(word, statusLabels[statusError]),
		Badge: statusLabels[statusError] + " · " + cmp.Or(word, stoppedDetail),
		Line:  failureLine(agentLabel(rec.Agent), rec.Error),
	}
}

func failureLine(agent, text string) string {
	line := firstLine(text)
	if m := exitStatusRe.FindStringSubmatch(line); m != nil {
		return agent + " exited with status " + m[1] + "."
	}
	if loc := acpErrorRe.FindStringIndex(line); loc != nil {
		return agent + " stopped: " + strings.TrimPrefix(line[loc[1]:], internalErrorPrefix)
	}
	return cmp.Or(line, agent+" stopped with an error.")
}

func failureActions(sid string, needsAuth, cannotResume bool) []failureAction {
	switch {
	case needsAuth:
		return []failureAction{{ID: recoverSignIn, Label: "Sign in", Icon: "person.badge.key", action: actSignIn}}
	case cannotResume:
		return []failureAction{continueAction(sid)}
	}
	return []failureAction{{ID: recoverRetry, Label: "Retry", Icon: "arrow.clockwise", Command: "agents.resume", Args: map[string]string{"session": sid}, action: actRetry}}
}

func reopenAction(sid string, cannotResume, task bool) failureAction {
	switch {
	case cannotResume:
		return continueAction(sid)
	case task:
		return failureAction{ID: recoverReopen, Label: "Restore", Icon: "arrow.clockwise", Command: "agents.restore", Args: map[string]string{"session": sid}, action: actRestore}
	}
	return failureAction{ID: recoverReopen, Label: "Reopen", Icon: "arrow.clockwise", Command: "agents.resume", Args: map[string]string{"session": sid}, action: actRetry}
}

func continueAction(sid string) failureAction {
	return failureAction{ID: recoverContinue, Label: "Hand off to a new session", Icon: "arrow.turn.down.right", Command: "agents.handoff", Args: map[string]string{"session": sid}, action: actContinue}
}
