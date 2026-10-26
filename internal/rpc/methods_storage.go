package rpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sourcegraph/jsonrpc2"
)

var errNoKV = errors.New("storage not configured")

type storageKeyParams struct {
	Key string `json:"key"`
}

type storageSetParams struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type storageListParams struct {
	Prefix string `json:"prefix"`
}

type storageGetResult struct {
	Value json.RawMessage `json:"value"`
	Found bool            `json:"found"`
}

type storageListResult struct {
	Keys []string `json:"keys"`
}

func (s *Server) storageMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"storage.get": method(func(ctx context.Context, p storageKeyParams) (storageGetResult, error) {
			plugin, err := s.storagePlugin(ctx)
			if err != nil {
				return storageGetResult{}, err
			}
			value, found, err := s.kv.Get(ctx, plugin, p.Key)
			if err != nil {
				return storageGetResult{}, err
			}
			if !found {
				value = json.RawMessage("null")
			}
			return storageGetResult{Value: value, Found: found}, nil
		}),
		"storage.set": method(func(ctx context.Context, p storageSetParams) (empty, error) {
			plugin, err := s.storagePlugin(ctx)
			if err != nil {
				return empty{}, err
			}
			return empty{}, s.kv.Set(ctx, plugin, p.Key, p.Value)
		}),
		"storage.delete": method(func(ctx context.Context, p storageKeyParams) (empty, error) {
			plugin, err := s.storagePlugin(ctx)
			if err != nil {
				return empty{}, err
			}
			return empty{}, s.kv.Delete(ctx, plugin, p.Key)
		}),
		"storage.list": method(func(ctx context.Context, p storageListParams) (storageListResult, error) {
			plugin, err := s.storagePlugin(ctx)
			if err != nil {
				return storageListResult{}, err
			}
			keys, err := s.kv.List(ctx, plugin, p.Prefix)
			return storageListResult{Keys: nonNil(keys)}, err
		}),
	}
}

func (s *Server) storagePlugin(ctx context.Context) (string, error) {
	caller := CallerFrom(ctx)
	if !caller.IsPlugin() {
		return "", &jsonrpc2.Error{Code: codeInvalidParams, Message: "storage requires a plugin caller"}
	}
	if s.kv == nil {
		return "", errNoKV
	}
	return caller.PluginID, nil
}
