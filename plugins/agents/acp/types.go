package acp

import "encoding/json"

const (
	ProtocolVersion = 1

	MethodInitialize        = "initialize"
	MethodSessionNew        = "session/new"
	MethodSessionResume     = "session/resume"
	MethodSessionPrompt     = "session/prompt"
	MethodSessionCancel     = "session/cancel"
	MethodSessionClose      = "session/close"
	MethodSessionDelete     = "session/delete"
	MethodSessionSetMode    = "session/set_mode"
	MethodSessionSetConfig  = "session/set_config_option"
	MethodSessionUpdate     = "session/update"
	MethodRequestPermission = "session/request_permission"
	MethodCancelRequest     = "$/cancel_request"
	MethodSessionSteering   = "_session/steering"

	UpdateUserMessageChunk  = "user_message_chunk"
	UpdateAgentMessageChunk = "agent_message_chunk"
	UpdateAgentThoughtChunk = "agent_thought_chunk"
	UpdateToolCall          = "tool_call"
	UpdateToolCallUpdate    = "tool_call_update"
	UpdatePlan              = "plan"
	UpdateAvailableCommands = "available_commands_update"
	UpdateCurrentMode       = "current_mode_update"
	UpdateConfigOption      = "config_option_update"
	UpdateSessionInfo       = "session_info_update"
	UpdateNotice            = "notice"

	ConfigSelect  = "select"
	CategoryMode  = "mode"
	CategoryModel = "model"

	StopEndTurn   = "end_turn"
	StopCancelled = "cancelled"

	OutcomeSelected  = "selected"
	OutcomeCancelled = "cancelled"

	CodeAuthRequired = -32000
	AuthTerminal     = "terminal"
)

type Capability struct{}

type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type InitializeRequest struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         *Implementation    `json:"clientInfo,omitempty"`
}

type ClientCapabilities struct {
	FS       FileSystemCapabilities    `json:"fs"`
	Terminal bool                      `json:"terminal"`
	Session  ClientSessionCapabilities `json:"session"`
	Auth     *AuthCapabilities         `json:"auth,omitempty"`
}

type AuthCapabilities struct {
	Terminal bool `json:"terminal"`
}

type AuthMethod struct {
	ID   string            `json:"id"`
	Name string            `json:"name"`
	Type string            `json:"type,omitempty"`
	Args []string          `json:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

type FileSystemCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type ClientSessionCapabilities struct {
	Notices *Capability `json:"notices,omitempty"`
}

type InitializeResponse struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AuthMethods       []json.RawMessage `json:"authMethods,omitempty"`
	AgentInfo         *Implementation   `json:"agentInfo,omitempty"`
	Meta              *InitializeMeta   `json:"_meta,omitempty"`
}

type InitializeMeta struct {
	Steering SteeringMeta `json:"steering"`
}

type SteeringMeta struct {
	Supported bool `json:"supported"`
}

const (
	SteerInjected       = "injected"
	SteerPromptRequired = "promptRequired"
)

type SteerRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
	Meta      SteerMeta      `json:"_meta"`
}

type SteerMeta struct {
	Steering SteerBehavior `json:"steering"`
}

type SteerBehavior struct {
	IdleBehavior string `json:"idleBehavior"`
}

type SteerResponse struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

func NewSteerRequest(sessionID string, prompt []ContentBlock) SteerRequest {
	return SteerRequest{SessionID: sessionID, Prompt: prompt, Meta: SteerMeta{Steering: SteerBehavior{IdleBehavior: SteerPromptRequired}}}
}

type AgentCapabilities struct {
	LoadSession         bool                `json:"loadSession,omitempty"`
	PromptCapabilities  PromptCapabilities  `json:"promptCapabilities"`
	SessionCapabilities SessionCapabilities `json:"sessionCapabilities"`
}

type PromptCapabilities struct {
	Image           bool `json:"image,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
}

type SessionCapabilities struct {
	List                  *Capability `json:"list,omitempty"`
	Resume                *Capability `json:"resume,omitempty"`
	Close                 *Capability `json:"close,omitempty"`
	Delete                *Capability `json:"delete,omitempty"`
	AdditionalDirectories *Capability `json:"additionalDirectories,omitempty"`
}

type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type McpServer struct {
	Name    string        `json:"name"`
	Command string        `json:"command"`
	Args    []string      `json:"args"`
	Env     []EnvVariable `json:"env"`
}

type NewSessionRequest struct {
	Cwd                   string       `json:"cwd"`
	AdditionalDirectories []string     `json:"additionalDirectories,omitempty"`
	McpServers            []McpServer  `json:"mcpServers"`
	Meta                  *SessionMeta `json:"_meta,omitempty"`
}

type SessionMeta struct {
	ClaudeCode ClaudeCodeMeta `json:"claudeCode"`
}

type ClaudeCodeMeta struct {
	Options ClaudeCodeOptions `json:"options"`
}

type ClaudeCodeOptions struct {
	Env      map[string]string   `json:"env,omitempty"`
	Settings *ClaudeCodeSettings `json:"settings,omitempty"`
}

type ClaudeCodeSettings struct {
	AdvisorModel string `json:"advisorModel,omitempty"`
}

type NewSessionResponse struct {
	SessionID     string                `json:"sessionId"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

type ResumeSessionRequest struct {
	SessionID             string       `json:"sessionId"`
	Cwd                   string       `json:"cwd"`
	AdditionalDirectories []string     `json:"additionalDirectories,omitempty"`
	McpServers            []McpServer  `json:"mcpServers"`
	Meta                  *SessionMeta `json:"_meta,omitempty"`
}

type ResumeSessionResponse struct {
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

type SessionModeState struct {
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SessionConfigOption struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Description  string                `json:"description,omitempty"`
	Category     string                `json:"category,omitempty"`
	Type         string                `json:"type"`
	CurrentValue json.RawMessage       `json:"currentValue"`
	Options      []SessionConfigChoice `json:"options,omitempty"`
}

type SessionConfigChoice struct {
	Value       string                `json:"value,omitempty"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Group       string                `json:"group,omitempty"`
	Options     []SessionConfigChoice `json:"options,omitempty"`
}

type SetSessionConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

type SetSessionConfigOptionResponse struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

type ConfigOptionUpdate struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

type SessionRequest struct {
	SessionID string `json:"sessionId"`
}

type SetSessionModeRequest struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

type PromptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type PromptResponse struct {
	StopReason string `json:"stopReason"`
}

type ContentBlock struct {
	Type     string                `json:"type"`
	Text     string                `json:"text,omitempty"`
	URI      string                `json:"uri,omitempty"`
	Name     string                `json:"name,omitempty"`
	Resource *TextResourceContents `json:"resource,omitempty"`
}

type TextResourceContents struct {
	URI      string `json:"uri"`
	Text     string `json:"text"`
	MimeType string `json:"mimeType,omitempty"`
}

type SessionNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

type UpdateHeader struct {
	SessionUpdate string `json:"sessionUpdate"`
}

type ContentChunk struct {
	Content   ContentBlock `json:"content"`
	MessageID string       `json:"messageId,omitempty"`
}

type ToolCallHeader struct {
	ToolCallID string `json:"toolCallId"`
	Kind       string `json:"kind,omitempty"`
	Status     string `json:"status,omitempty"`
}

type AvailableCommand struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input,omitempty"`
}

type AvailableCommandsUpdate struct {
	AvailableCommands []AvailableCommand `json:"availableCommands"`
}

type CurrentModeUpdate struct {
	CurrentModeID string `json:"currentModeId"`
}

type SessionInfoUpdate struct {
	Title *string `json:"title"`
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type RequestPermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  json.RawMessage    `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

type CancelRequestNotification struct {
	RequestID json.RawMessage `json:"requestId"`
}

type RequestPermissionResponse struct {
	Outcome PermissionOutcome `json:"outcome"`
}

func (o SessionConfigOption) Current() string {
	var v string
	_ = json.Unmarshal(o.CurrentValue, &v)
	return v
}

func (o SessionConfigOption) Choices() []SessionConfigChoice {
	var out []SessionConfigChoice
	for _, c := range o.Options {
		if len(c.Options) > 0 {
			out = append(out, c.Options...)
			continue
		}
		out = append(out, c)
	}
	return out
}

func (o SessionConfigOption) Choice(value string) (SessionConfigChoice, bool) {
	for _, c := range o.Choices() {
		if c.Value == value {
			return c, true
		}
	}
	return SessionConfigChoice{}, false
}
