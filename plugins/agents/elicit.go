package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
)

const (
	entryElicitation = "elicitation"

	kindElicitation         = "elicitation/create"
	kindElicitationAnswer   = "elicitation"
	kindElicitationComplete = "elicitation/complete"

	fieldChoice  = "choice"
	fieldMulti   = "multi"
	fieldText    = "text"
	fieldNumber  = "number"
	fieldInteger = "integer"
	fieldBoolean = "boolean"

	maxEarlyCompletions = 64
)

var (
	errElicitAction     = errors.New("action must be accept, decline or cancel")
	errMissingAnswer    = errors.New("an answer is required")
	errBadAnswer        = errors.New("answer does not match the field")
	errUnsupportedForm  = errors.New("unsupported elicitation")
	errUnsafeURL        = errors.New("only http and https links open in the browser")
	errNotAnElicitation = errors.New("not a pending question of this session")
)

type elicitation struct {
	Mode          string        `json:"mode"`
	Message       string        `json:"message"`
	URL           string        `json:"url,omitempty"`
	ElicitationID string        `json:"elicitationId,omitempty"`
	ToolCallID    string        `json:"toolCallId,omitempty"`
	Fields        []elicitField `json:"fields,omitempty"`
}

type elicitField struct {
	Key         string         `json:"key"`
	Type        string         `json:"type"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Options     []elicitOption `json:"options,omitempty"`
}

type elicitOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type elicitationPayload struct {
	ID        string          `json:"id"`
	RequestID string          `json:"requestId"`
	Params    json.RawMessage `json:"params"`
}

type elicitationAnswerPayload struct {
	Request  string                  `json:"request"`
	Response acp.ElicitationResponse `json:"response"`
}

type elicitationCompletePayload struct {
	Request string `json:"request"`
}

type waitElicitation struct {
	Request string        `json:"request"`
	Mode    string        `json:"mode"`
	Message string        `json:"message"`
	URL     string        `json:"url,omitempty"`
	Fields  []elicitField `json:"fields,omitempty"`
}

type elicitationArgs struct {
	Session string        `json:"session"`
	Request string        `json:"request"`
	Action  string        `json:"action"`
	Answers []answerValue `json:"answers"`
}

type answerValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type schemaProperty struct {
	Type        string       `json:"type"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Enum        []string     `json:"enum"`
	OneOf       []enumOption `json:"oneOf"`
	Items       *struct {
		Enum  []string     `json:"enum"`
		AnyOf []enumOption `json:"anyOf"`
	} `json:"items"`
}

type enumOption struct {
	Const       string `json:"const"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (p *agentProc) elicit(ctx context.Context, request string, params json.RawMessage) acp.ElicitationResponse {
	var req acp.CreateElicitationRequest
	if json.Unmarshal(params, &req) != nil || req.SessionID == "" {
		return acp.ElicitationResponse{Action: acp.ElicitationCancel}
	}
	if _, err := parseElicitation(params); err != nil {
		return acp.ElicitationResponse{Action: acp.ElicitationCancel}
	}
	p.mu.Lock()
	s := p.ownerLocked(req.SessionID)
	done := p.elicited[req.ElicitationID]
	delete(p.elicited, req.ElicitationID)
	p.mu.Unlock()
	switch {
	case s == nil:
		return acp.ElicitationResponse{Action: acp.ElicitationCancel}
	case done && req.Mode == acp.ElicitationURL:
		return acp.ElicitationResponse{Action: acp.ElicitationAccept}
	}
	return s.requestElicitation(ctx, request, params)
}

func (p *agentProc) completeElicitation(params json.RawMessage) {
	var n acp.CompleteElicitationNotification
	if json.Unmarshal(params, &n) != nil {
		return
	}
	for _, s := range p.attached() {
		if s.completeElicitation(n.ElicitationID) {
			return
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.elicited == nil || len(p.elicited) >= maxEarlyCompletions {
		p.elicited = map[string]bool{}
	}
	p.elicited[n.ElicitationID] = true
}

func (a *app) elicitationCommand(_ context.Context, cmd pluginsdk.CommandRun, args elicitationArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	if !asUser(cmd) && (cmd.Caller == "" || !a.descendantOf(args.Session, cmd.Caller)) {
		return nil, errNotDescendant
	}
	el, ok := s.elicitation(args.Request)
	if !ok {
		return nil, errNotAnElicitation
	}
	return nil, s.respond(args.Request, acp.ElicitationResponse{Action: args.Action, Content: answersContent(el, args.Answers)})
}

func (s *session) requestElicitation(ctx context.Context, request string, params json.RawMessage) acp.ElicitationResponse {
	perm := &pendingPermission{request: request, at: time.Now().UTC(), reply: make(chan acp.PermissionOutcome, 1), elicit: make(chan acp.ElicitationResponse, 1)}
	s.mu.Lock()
	s.loadLocked()
	id := entryElicitation + "-" + strconv.Itoa(s.nextID+1)
	s.perms[id] = perm
	s.recordLocked(s.actor(), kindElicitation, elicitationPayload{ID: id, RequestID: request, Params: params})
	s.settleLocked()
	waiting := s.turnLocked()
	s.mu.Unlock()
	s.turned(waiting)
	s.changed(changeStatus, true)
	if s.requested != nil {
		s.requested(entryElicitation, id)
	}
	out := acp.ElicitationResponse{Action: acp.ElicitationCancel}
	select {
	case out = <-perm.elicit:
	case <-perm.reply:
	case <-ctx.Done():
	}
	s.mu.Lock()
	delete(s.perms, id)
	s.recordLocked(actorUser, kindElicitationAnswer, elicitationAnswerPayload{Request: id, Response: out})
	s.settleLocked()
	s.mu.Unlock()
	s.changed(changeStatus, true)
	if s.resolved != nil {
		s.resolved(entryElicitation, id)
	}
	return out
}

func (s *session) respond(id string, resp acp.ElicitationResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	perm, ok := s.perms[id]
	i, found := s.index[key(entryElicitation, id)]
	if !ok || perm.elicit == nil || !found {
		return errNotAnElicitation
	}
	out, err := checkResponse(s.entries[i].Elicitation, resp)
	if err != nil {
		return err
	}
	delete(s.perms, id)
	perm.elicit <- out
	return nil
}

func (s *session) completeElicitation(elicitationID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded || elicitationID == "" {
		return false
	}
	for _, en := range slices.Backward(s.entries) {
		if en.Kind != entryElicitation || en.Elicitation == nil || en.Elicitation.ElicitationID != elicitationID {
			continue
		}
		if perm := s.perms[en.ID]; perm != nil {
			delete(s.perms, en.ID)
			perm.elicit <- acp.ElicitationResponse{Action: acp.ElicitationAccept}
		}
		if !en.Completed {
			s.recordLocked(actorClubmate, kindElicitationComplete, elicitationCompletePayload{Request: en.ID})
		}
		return true
	}
	return false
}

func (s *session) asking() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, perm := range s.perms {
		if perm.elicit != nil {
			return true
		}
	}
	return false
}

func (s *session) elicitation(id string) (elicitation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.index[key(entryElicitation, id)]
	if !ok || s.entries[i].Elicitation == nil {
		return elicitation{}, false
	}
	return *s.entries[i].Elicitation, true
}

func (s *session) elicitationLocked() *waitElicitation {
	for _, en := range s.entries {
		if en.Kind == entryElicitation && en.Elicitation != nil && s.perms[en.ID] != nil {
			el := en.Elicitation
			return &waitElicitation{Request: en.ID, Mode: el.Mode, Message: el.Message, URL: el.URL, Fields: slices.Clone(el.Fields)}
		}
	}
	return nil
}

func (s *session) applyElicitation(e envelope) {
	var p elicitationPayload
	if json.Unmarshal(e.Payload, &p) != nil {
		return
	}
	el, err := parseElicitation(p.Params)
	if err != nil {
		return
	}
	s.upsert(entryElicitation, p.ID, e.Turn).Elicitation = el
}

func (s *session) applyElicitationAnswer(e envelope) {
	var p elicitationAnswerPayload
	if json.Unmarshal(e.Payload, &p) != nil {
		return
	}
	if i, ok := s.index[key(entryElicitation, p.Request)]; ok {
		s.entries[i].Response, s.entries[i].AnsweredAt, s.entries[i].node = &p.Response, e.TS, nil
	}
}

func (s *session) applyElicitationComplete(e envelope) {
	var p elicitationCompletePayload
	if json.Unmarshal(e.Payload, &p) != nil {
		return
	}
	if i, ok := s.index[key(entryElicitation, p.Request)]; ok {
		s.entries[i].Completed, s.entries[i].node = true, nil
	}
}

func parseElicitation(params json.RawMessage) (*elicitation, error) {
	var req acp.CreateElicitationRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	el := &elicitation{Mode: req.Mode, Message: req.Message, ToolCallID: req.ToolCallID}
	switch req.Mode {
	case acp.ElicitationURL:
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, errUnsafeURL
		}
		el.URL, el.ElicitationID = req.URL, req.ElicitationID
		return el, nil
	case acp.ElicitationForm:
		fields, err := schemaFields(req.RequestedSchema)
		if err != nil {
			return nil, err
		}
		el.Fields = fields
		return el, nil
	}
	return nil, fmt.Errorf("%w: mode %q", errUnsupportedForm, req.Mode)
}

func answersContent(el elicitation, answers []answerValue) map[string]any {
	content := make(map[string]any, len(answers))
	for _, f := range el.Fields {
		for _, a := range answers {
			if a.Key != f.Key {
				continue
			}
			switch f.Type {
			case fieldMulti:
				list, _ := content[f.Key].([]any)
				content[f.Key] = append(list, a.Value)
			case fieldBoolean:
				content[f.Key] = a.Value == "true"
			default:
				content[f.Key] = a.Value
			}
		}
	}
	return content
}

func checkResponse(el *elicitation, resp acp.ElicitationResponse) (acp.ElicitationResponse, error) {
	switch resp.Action {
	case acp.ElicitationDecline, acp.ElicitationCancel:
		return acp.ElicitationResponse{Action: resp.Action}, nil
	case acp.ElicitationAccept:
	default:
		return acp.ElicitationResponse{}, errElicitAction
	}
	if el == nil || el.Mode != acp.ElicitationForm {
		return acp.ElicitationResponse{Action: acp.ElicitationAccept}, nil
	}
	content := map[string]any{}
	for _, f := range el.Fields {
		v, ok, err := fieldValue(f, resp.Content[f.Key])
		if err != nil {
			return acp.ElicitationResponse{}, fmt.Errorf("%s: %w", f.label(), err)
		}
		if !ok {
			if f.Required {
				return acp.ElicitationResponse{}, fmt.Errorf("%s: %w", f.label(), errMissingAnswer)
			}
			continue
		}
		content[f.Key] = v
	}
	return acp.ElicitationResponse{Action: acp.ElicitationAccept, Content: content}, nil
}

func fieldValue(f elicitField, v any) (any, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	switch f.Type {
	case fieldChoice:
		s, ok := v.(string)
		if !ok {
			return nil, false, errBadAnswer
		}
		if s == "" {
			return nil, false, nil
		}
		if !slices.ContainsFunc(f.Options, func(o elicitOption) bool { return o.Value == s }) {
			return nil, false, errUnknownOption
		}
		return s, true, nil
	case fieldMulti:
		list, ok := v.([]any)
		if !ok {
			return nil, false, errBadAnswer
		}
		picks := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok || !slices.ContainsFunc(f.Options, func(o elicitOption) bool { return o.Value == s }) {
				return nil, false, errUnknownOption
			}
			picks = append(picks, s)
		}
		return picks, len(picks) > 0, nil
	case fieldText:
		s, ok := v.(string)
		if !ok {
			return nil, false, errBadAnswer
		}
		return s, strings.TrimSpace(s) != "", nil
	case fieldNumber, fieldInteger:
		return numberValue(f.Type, v)
	case fieldBoolean:
		b, ok := v.(bool)
		if !ok {
			return nil, false, errBadAnswer
		}
		return b, true, nil
	}
	return nil, false, nil
}

func numberValue(kind string, v any) (any, bool, error) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case string:
		if strings.TrimSpace(x) == "" {
			return nil, false, nil
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return nil, false, errBadAnswer
		}
		n = parsed
	default:
		return nil, false, errBadAnswer
	}
	if kind == fieldInteger {
		if n != math.Trunc(n) {
			return nil, false, errBadAnswer
		}
		return int64(n), true, nil
	}
	return n, true, nil
}

func schemaFields(raw json.RawMessage) ([]elicitField, error) {
	var schema struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	if len(schema.Properties) == 0 || string(schema.Properties) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(schema.Properties))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("%w: properties is not an object", errUnsupportedForm)
	}
	var fields []elicitField
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := tok.(string)
		var p schemaProperty
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		f, ok := p.field(name)
		if !ok {
			if slices.Contains(schema.Required, name) {
				return nil, fmt.Errorf("%w: field %q of type %q", errUnsupportedForm, name, p.Type)
			}
			continue
		}
		f.Required = slices.Contains(schema.Required, name)
		fields = append(fields, f)
	}
	return fields, nil
}

func (p schemaProperty) field(name string) (elicitField, bool) {
	f := elicitField{Key: name, Type: p.Type, Title: p.Title, Description: p.Description}
	switch p.Type {
	case "string":
		f.Options = enumOptions(p.Enum, p.OneOf)
		f.Type = fieldText
		if len(f.Options) > 0 {
			f.Type = fieldChoice
		}
	case "array":
		if p.Items == nil {
			return elicitField{}, false
		}
		f.Type, f.Options = fieldMulti, enumOptions(p.Items.Enum, p.Items.AnyOf)
		if len(f.Options) == 0 {
			return elicitField{}, false
		}
	case fieldNumber, fieldInteger, fieldBoolean:
	default:
		return elicitField{}, false
	}
	return f, true
}

func (f elicitField) label() string {
	if f.Title != "" {
		return f.Title
	}
	return f.Key
}

func enumOptions(values []string, titled []enumOption) []elicitOption {
	out := make([]elicitOption, 0, len(values)+len(titled))
	for _, o := range titled {
		out = append(out, elicitOption{Value: o.Const, Label: cmp.Or(o.Title, o.Const), Description: o.Description})
	}
	for _, v := range values {
		out = append(out, elicitOption{Value: v, Label: v})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func openInBrowser(link string) error {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errUnsafeURL
	}
	return exec.Command("open", u.String()).Run()
}
