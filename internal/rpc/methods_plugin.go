package rpc

import "context"

func (s *Server) SetPluginList(fn func() any) {
	s.pluginList.Store(&fn)
}

func (s *Server) SetPluginRestart(fn func(id string) error) {
	s.pluginRestart.Store(&fn)
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
		"plugin.restart": method(s.restartPlugin),
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
