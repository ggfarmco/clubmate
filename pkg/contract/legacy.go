package contract

import (
	"bytes"
	"encoding/json"
	"os"
)

const (
	LegacyWorkspaceIDKey = "projectId"
	WorkspaceIDKey       = "workspaceId"
)

func UpgradeLegacyKeys(raw []byte) []byte {
	if !bytes.Contains(raw, []byte(`"`+LegacyWorkspaceIDKey+`"`)) {
		return raw
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return raw
	}
	legacy, ok := fields[LegacyWorkspaceIDKey]
	if !ok {
		return raw
	}
	delete(fields, LegacyWorkspaceIDKey)
	if _, ok := fields[WorkspaceIDKey]; !ok {
		fields[WorkspaceIDKey] = legacy
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return out
}

const (
	WorkspaceEnv       = "CLUBMATE_WORKSPACE_ID"
	LegacyWorkspaceEnv = "CLUBMATE_PROJECT_ID"
)

func WorkspaceFromEnv() string {
	if id := os.Getenv(WorkspaceEnv); id != "" {
		return id
	}
	return os.Getenv(LegacyWorkspaceEnv)
}
