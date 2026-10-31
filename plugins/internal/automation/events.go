package automation

import "clubmate/pkg/pluginsdk"

const (
	TopicProjectCreated = "project.created"
	TopicProjectDeleted = "project.deleted"
)

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
