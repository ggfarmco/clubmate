package rpc

import "context"

type Caller struct {
	PluginID string
}

type callerKey struct{}

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
