package contract

const CodePluginError = -32013

type PluginErrorData struct {
	Plugin  string `json:"plugin"`
	Method  string `json:"method"`
	Message string `json:"message"`
}
