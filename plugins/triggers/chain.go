package main

import (
	"cmp"
	"context"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	sessionPrefix = "agt_"
	agentsPrefix  = "agents."
)

func (a *app) chain(ctx context.Context, workspace string, ev pluginsdk.Event, sessions []api.Session) *automation.Chain {
	if ev.Topic == manualTopic {
		return &automation.Chain{User: true}
	}
	payload := decodePayload(ev.Payload)
	field := func(path string) string {
		v, _ := lookup(payload, path)
		s, _ := v.(string)
		return s
	}
	var c automation.Chain
	switch {
	case strings.HasPrefix(ev.Topic, emitPrefix):
		c.CronJob = field(cronField)
		if by := field(byField); strings.HasPrefix(by, sessionPrefix) {
			c.Session = by
		}
	case strings.HasPrefix(ev.Topic, agentsPrefix):
		c.Session, c.CronJob, c.Trigger, c.Status = field(sessionField), field(cronField), field(triggerField), field(statusField)
		if n, ok := lookup(payload, turnField); ok {
			if f, ok := n.(float64); ok {
				c.Turn = int(f)
			}
		}
	}
	if c.Session != "" {
		if sessions == nil {
			var err error
			if sessions, err = a.sessions(ctx, workspace); err != nil {
				logError(err)
			}
		}
		if s, ok := agentcall.Find(sessions, c.Session); ok {
			c.Title = s.Title
			c.CronJob = cmp.Or(c.CronJob, s.CronJob)
			c.Trigger = cmp.Or(c.Trigger, s.Trigger)
		}
	}
	if c.CronJob != "" {
		c.CronName = a.cronName(ctx, workspace, c.CronJob)
	}
	if c.Trigger != "" {
		a.mu.Lock()
		if t, ok := a.triggers[c.Trigger]; ok && t.Workspace == workspace {
			c.TriggerName = t.Name
		}
		a.mu.Unlock()
	}
	if c == (automation.Chain{}) {
		return nil
	}
	return &c
}

func (a *app) cronJobName(ctx context.Context, workspace, id string) string {
	var job struct {
		Name string `json:"name"`
	}
	if err := a.host.RunCommand(ctx, "cron.show", workspace, automation.IDArgs{ID: id}, &job); err != nil {
		logError(err)
		return ""
	}
	return job.Name
}

func eventWords(r Run, titles map[string]string) string {
	c := r.Chain
	if c == nil {
		return topicPhrase(r.Topic)
	}
	session := ""
	if c.Session != "" {
		session = cmp.Or(titles[c.Session], c.Title, automation.Names(c.Session, titles))
	}
	var parts []string
	switch {
	case r.Topic == manualTopic:
		parts = append(parts, "run now by you")
	case r.Topic == api.TopicTurn && session != "":
		turn := session + "'s turn ended"
		if c.Turn > 0 {
			turn = session + "'s turn " + strconv.Itoa(c.Turn) + " ended"
		}
		parts = append(parts, turn)
		if c.Status != "" {
			parts = append(parts, c.Status)
		}
	case strings.HasPrefix(r.Topic, emitPrefix):
		emitted := "“" + strings.TrimPrefix(r.Topic, emitPrefix) + "” emitted"
		if session != "" {
			emitted += " by " + session
		}
		parts = append(parts, emitted)
	default:
		parts = append(parts, topicPhrase(r.Topic))
		if session != "" {
			parts = append(parts, "session "+session)
		}
	}
	switch {
	case c.CronJob != "":
		parts = append(parts, "from cron "+cmp.Or(c.CronName, "a deleted cron job"))
	case c.Trigger != "":
		parts = append(parts, "from trigger "+cmp.Or(c.TriggerName, "a deleted trigger"))
	}
	return strings.Join(parts, " · ")
}
