package main

import (
	"cmp"
	"encoding/json"
	"errors"
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
	failureOverload  = "overloaded"
	failureProvider  = "provider_error"
	failureQuota     = "quota_exhausted"
	failureRateLimit = "rate_limited"
	failureTransport = "transport_lost"
	failureShutdown  = "worker_shutdown"

	internalErrorPrefix = "Internal error: "
	netErrorTitle       = "Network error."
	newSessionHint      = "Start a new session or hand this one off to continue."
)

type turnFailure struct {
	kind       string
	title      string
	detail     string
	retry      bool
	newSession bool
}

var (
	netErrors = []string{"No response from API", "Connection refused", "ECONNREFUSED"}

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
		failureInternal:  {title: "Claude Agent encountered an internal error.", retry: true},
		failureOverload:  {title: "Claude is temporarily overloaded.", retry: true},
		failureProvider:  {title: "The model provider reported an error.", retry: true},
		failureQuota:     {title: "The Claude account has no available quota."},
		failureRateLimit: {title: "Claude is temporarily rate limited.", retry: true},
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
	var data acp.ErrorData
	if json.Unmarshal(rpcErr.Data, &data) == nil && data.ErrorKind != "" {
		kind := cmp.Or(errorKinds[data.ErrorKind], failureProvider)
		f := failurePolicies[kind]
		f.kind, f.detail = kind, detail
		return f, true
	}
	if slices.ContainsFunc(netErrors, func(m string) bool { return strings.Contains(rpcErr.Message, m) }) {
		return turnFailure{title: netErrorTitle, detail: detail, retry: true}, true
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
