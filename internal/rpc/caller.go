package rpc

import "context"

type Caller struct {
	PluginID string
	PID      int
}

type callerKey struct{}

type agentCallKey struct{}

type originKey struct{}

type origin struct {
	caller   string
	terminal string
}

func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

func CallerFrom(ctx context.Context) Caller {
	c, _ := ctx.Value(callerKey{}).(Caller)
	return c
}

func (c Caller) IsPlugin() bool {
	return c.PluginID != ""
}

func (c Caller) String() string {
	if c.IsPlugin() {
		return "plugin:" + c.PluginID
	}
	return "cli"
}

func withAgentCall(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentCallKey{}, true)
}

func isAgentCall(ctx context.Context) bool {
	agent, _ := ctx.Value(agentCallKey{}).(bool)
	return agent
}

func withOrigin(ctx context.Context, caller, terminal string) context.Context {
	return context.WithValue(ctx, originKey{}, origin{caller: caller, terminal: terminal})
}

func originFrom(ctx context.Context) origin {
	o, _ := ctx.Value(originKey{}).(origin)
	return o
}
