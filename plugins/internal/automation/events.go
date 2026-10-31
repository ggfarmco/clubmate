package automation

import "clubmate/pkg/pluginsdk"

const (
	TopicAgentsChanged  = "agents.changed"
	TopicAgentsReady    = "agents.ready"
	TopicProjectCreated = "project.created"
	TopicProjectDeleted = "project.deleted"
	changeStatus        = "status"
	changeDeleted       = "deleted"
)

type changedPayload struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Kind      string `json:"kind"`
}

type createdPayload struct {
	Project pluginsdk.Project `json:"project"`
}

type deletedPayload struct {
	ID string `json:"id"`
}

type notifyRequest struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
}
