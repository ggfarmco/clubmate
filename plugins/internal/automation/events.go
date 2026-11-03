package automation

const (
	TopicWorkspaceCreated = "workspace.created"
	TopicWorkspaceDeleted = "workspace.deleted"
)

type NotifyRequest struct {
	WorkspaceID string `json:"workspaceId"`
	Title       string `json:"title"`
	Message     string `json:"message"`
	Kind        string `json:"kind"`
}
