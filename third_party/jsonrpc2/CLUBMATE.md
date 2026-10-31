# Fork of github.com/sourcegraph/jsonrpc2 v0.2.3

Wired in through `replace` in the root `go.mod`. Changes against upstream (`wire.go`, `MarshalJSON`/`UnmarshalJSON` in `request.go`, `response.go`, `conn.go`):

- Requests and responses are encoded by appending fields into one buffer; `params`, `result` and `meta` bytes are spliced in as they are (compacted only when they contain a newline). Upstream marshalled every frame three times through a `map[string]any`, and the v2-backed `encoding/json` reformats a `RawMessage` on every pass.
- Requests are decoded in one `jsontext` pass; `params` stay raw instead of going through `any` and back.
- `NewPlainObjectStream` frames messages itself: a 32 KB `bufio.Reader`, a reused read and write buffer, dropped after a message over 1 MB, so a connection does not keep its largest message forever.
- `SetParams` and `SetResult` take a `json.RawMessage` without re-encoding it.

Run its tests with `cd third_party/jsonrpc2 && go test ./...` (a nested module, outside `go test ./...` of the root).
