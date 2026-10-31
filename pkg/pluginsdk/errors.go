package pluginsdk

import (
	"encoding/json"
	"errors"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/contract"
)

type PluginError = contract.PluginErrorData

func PluginErrorOf(err error) (PluginError, bool) {
	rpcErr, ok := errors.AsType[*jsonrpc2.Error](err)
	if !ok || rpcErr.Code != contract.CodePluginError || rpcErr.Data == nil {
		return PluginError{}, false
	}
	var pe PluginError
	if json.Unmarshal(*rpcErr.Data, &pe) != nil || pe.Plugin == "" {
		return PluginError{}, false
	}
	return pe, true
}
