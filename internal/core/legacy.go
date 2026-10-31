package core

import (
	"context"

	"clubmate/internal/workspace"
	"clubmate/pkg/contract"
)

var legacyWorkspaceTopics = map[string]string{
	contract.TopicWorkspaceCreated: "project.created",
	contract.TopicWorkspaceUpdated: "project.updated",
	contract.TopicWorkspaceDeleted: "project.deleted",
}

type legacyWorkspaceChanged struct {
	Project workspace.Workspace `json:"project"`
}

type legacyWorkspaceDeleted struct {
	ID string `json:"id"`
}

func (s *Service) publishWorkspace(ctx context.Context, topic string, w workspace.Workspace) {
	s.publish(ctx, topic, contract.WorkspaceChanged{Workspace: w.ID, Info: w})
	s.publish(ctx, legacyWorkspaceTopics[topic], legacyWorkspaceChanged{Project: w})
}

func (s *Service) publishWorkspaceDeleted(ctx context.Context, id string) {
	s.publish(ctx, contract.TopicWorkspaceDeleted, contract.WorkspaceDeleted{Workspace: id})
	s.publish(ctx, legacyWorkspaceTopics[contract.TopicWorkspaceDeleted], legacyWorkspaceDeleted{ID: id})
}
