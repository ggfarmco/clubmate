package rpc

import "context"

type PluginControl interface {
	Enabled(id string) bool
	SetEnabled(ctx context.Context, id string, enabled bool) error
	Providers(capability string) CapabilityProviders
	SetDefault(ctx context.Context, capability, pluginID string) error
}

type CapabilityProviders struct {
	Default   string               `json:"default"`
	Providers []CapabilityProvider `json:"providers"`
}

type CapabilityProvider struct {
	Plugin  string `json:"plugin"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type setEnabledParams struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type capabilityParams struct {
	Name string `json:"name"`
}

type setDefaultParams struct {
	Name   string `json:"name"`
	Plugin string `json:"plugin"`
}

func (s *Server) SetPluginList(fn func() any) {
	s.pluginList.Store(&fn)
}

func (s *Server) SetPluginRestart(fn func(id string) error) {
	s.pluginRestart.Store(&fn)
}

func (s *Server) SetPluginControl(c PluginControl) {
	s.pluginControl.Store(&c)
}

func (s *Server) pluginMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"plugin.list": method(func(context.Context, empty) (any, error) {
			fn := s.pluginList.Load()
			if fn == nil {
				return []struct{}{}, nil
			}
			return (*fn)(), nil
		}),
		"plugin.restart":         method(s.restartPlugin),
		"plugin.set-enabled":     method(s.setPluginEnabled),
		"capability.providers":   method(s.capabilityProviders),
		"capability.set-default": method(s.setCapabilityDefault),
	}
}

func (s *Server) restartPlugin(ctx context.Context, p idParams) (empty, error) {
	if CallerFrom(ctx).IsPlugin() {
		return empty{}, invalidParams("plugin.restart is not available to plugins")
	}
	fn := s.pluginRestart.Load()
	if fn == nil {
		return empty{}, ErrUnknownPlugin
	}
	return empty{}, (*fn)(p.ID)
}

func (s *Server) setPluginEnabled(ctx context.Context, p setEnabledParams) (empty, error) {
	if CallerFrom(ctx).IsPlugin() {
		return empty{}, invalidParams("plugin.set-enabled is not available to plugins")
	}
	c := s.pluginControl.Load()
	if c == nil {
		return empty{}, ErrUnknownPlugin
	}
	return empty{}, (*c).SetEnabled(ctx, p.ID, p.Enabled)
}

func (s *Server) capabilityProviders(_ context.Context, p capabilityParams) (CapabilityProviders, error) {
	if p.Name == "" {
		return CapabilityProviders{}, invalidParams("name is required")
	}
	c := s.pluginControl.Load()
	if c == nil {
		return CapabilityProviders{Providers: []CapabilityProvider{}}, nil
	}
	return (*c).Providers(p.Name), nil
}

func (s *Server) setCapabilityDefault(ctx context.Context, p setDefaultParams) (empty, error) {
	if CallerFrom(ctx).IsPlugin() {
		return empty{}, invalidParams("capability.set-default is not available to plugins")
	}
	if p.Name == "" || p.Plugin == "" {
		return empty{}, invalidParams("name and plugin are required")
	}
	c := s.pluginControl.Load()
	if c == nil {
		return empty{}, ErrUnknownPlugin
	}
	return empty{}, (*c).SetDefault(ctx, p.Name, p.Plugin)
}

func (s *Server) pluginEnabled(id string) bool {
	c := s.pluginControl.Load()
	return c == nil || (*c).Enabled(id)
}
