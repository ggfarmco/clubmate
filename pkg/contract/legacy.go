package contract

import (
	"bytes"
	"encoding/json"
	"maps"
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

var legacyCommands = map[string]string{
	"agents.afkDefault":    "agents.afk-default",
	"agents.finishDefault": "agents.finish-default",
	"remote.setPort":       "remote.set-port",
	"remote.cancelPair":    "remote.cancel-pair",
}

func CurrentCommand(id string) string {
	if current, ok := legacyCommands[id]; ok {
		return current
	}
	return id
}

func LegacyCommands() map[string]string {
	return maps.Clone(legacyCommands)
}
