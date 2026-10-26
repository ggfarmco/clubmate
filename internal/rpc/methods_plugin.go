package rpc

import "context"

func (s *Server) SetPluginList(fn func() any) {
	s.pluginList.Store(&fn)
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
	}
}
