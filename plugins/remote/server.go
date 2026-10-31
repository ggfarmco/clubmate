package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

const (
	maxBodyBytes      = 1 << 20
	maxDeviceField    = 128
	requestTimeout    = 2 * time.Minute
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 2 * time.Minute
	maxHeaderBytes    = 64 << 10
	jsonrpcInvalid    = -32602
)

const (
	codeUnauthorized  = "unauthorized"
	codePairingFailed = "pairing_failed"
	codeNotAllowed    = "not_allowed"
	codeInvalidParams = "invalid_params"
	codeFailed        = "failed"
)

var errStreamClosed = errors.New("stream closed")

type server struct {
	backend  backend
	devices  *deviceStore
	pairing  *pairing
	hub      *hub
	computer func() computer
	changed  func()
	ping     time.Duration
}

type computer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorBody struct {
	Error apiError `json:"error"`
}

type resultBody struct {
	Result any `json:"result"`
}

type pairRequest struct {
	Code   string     `json:"code"`
	Device deviceInfo `json:"device"`
}

type deviceInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
}

type pairResponse struct {
	Token    string   `json:"token"`
	Computer computer `json:"computer"`
}

type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type commandRequest struct {
	ID        string          `json:"id"`
	Workspace string          `json:"workspace"`
	Args      json.RawMessage `json:"args"`
}

type helloResult struct {
	Computer computer `json:"computer"`
	Version  string   `json:"version"`
	Commands []string `json:"commands"`
	Topics   []string `json:"topics"`
}

type workspaceInfo struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Roots []string `json:"roots"`
}

type authedHandler func(w http.ResponseWriter, r *http.Request, d device)

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pair", s.pair)
	mux.HandleFunc("POST /v1/rpc", s.authed(s.rpc))
	mux.HandleFunc("GET /v1/events", s.authed(s.events))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		mux.ServeHTTP(w, r)
	})
}

func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

func (s *server) pair(w http.ResponseWriter, r *http.Request) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(requestTimeout))
	var req pairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParams, "invalid request body")
		return
	}
	if !validDevice(req.Device) {
		writeError(w, http.StatusBadRequest, codeInvalidParams, "device id, name and model must be at most 128 characters; id is required")
		return
	}
	gen, ok := s.pairing.redeem(req.Code, remoteHost(r.RemoteAddr))
	if !ok {
		writeError(w, http.StatusForbidden, codePairingFailed, "the pairing code is wrong, expired or already used")
		return
	}
	token, err := s.devices.add(r.Context(), req.Device.ID, req.Device.Name, req.Device.Model)
	if err != nil {
		s.pairing.reopen(gen)
		writeError(w, http.StatusInternalServerError, codeFailed, err.Error())
		return
	}
	s.hub.revoke(req.Device.ID)
	s.changed()
	writeJSON(w, http.StatusOK, pairResponse{Token: token, Computer: s.computer()})
}

func (s *server) revoke(ctx context.Context, id string) error {
	if err := s.devices.revoke(ctx, id); err != nil {
		return err
	}
	s.hub.revoke(id)
	s.changed()
	return nil
}

func (s *server) authed(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "missing device token")
			return
		}
		d, ok := s.devices.authenticate(r.Context(), token)
		if !ok {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "unknown or revoked device token")
			return
		}
		next(w, r, d)
	}
}

func (s *server) rpc(w http.ResponseWriter, r *http.Request, _ device) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(requestTimeout))
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, errorBody{Error: apiError{Code: codeInvalidParams, Message: "invalid request body"}})
		return
	}
	result, err := s.call(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusOK, errorBody{Error: toAPIError(err)})
		return
	}
	writeJSON(w, http.StatusOK, resultBody{Result: result})
}

func (s *server) call(ctx context.Context, req rpcRequest) (any, error) {
	switch req.Method {
	case "hello":
		return s.hello(ctx)
	case "workspaces":
		return s.workspaces(ctx)
	case "command":
		var p commandRequest
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return s.command(ctx, p)
	}
	return nil, &apiError{Code: codeNotAllowed, Message: "unknown method: " + req.Method}
}

func (s *server) hello(ctx context.Context) (helloResult, error) {
	cmds, err := s.remoteCommands(ctx)
	if err != nil {
		return helloResult{}, err
	}
	version, err := s.backend.Version(ctx)
	if err != nil {
		return helloResult{}, err
	}
	return helloResult{Computer: s.computer(), Version: version, Commands: cmds, Topics: slices.Clone(forwardedTopics)}, nil
}

func (s *server) workspaces(ctx context.Context) ([]workspaceInfo, error) {
	projects, err := s.backend.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]workspaceInfo, 0, len(projects))
	for _, p := range projects {
		roots := p.Roots
		if roots == nil {
			roots = []string{}
		}
		out = append(out, workspaceInfo{ID: p.ID, Name: p.Name, Roots: roots})
	}
	return out, nil
}

func (s *server) command(ctx context.Context, p commandRequest) (json.RawMessage, error) {
	cmds, err := s.remoteCommands(ctx)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(cmds, p.ID) {
		return nil, &apiError{Code: codeNotAllowed, Message: "command is not available remotely: " + p.ID}
	}
	out, err := s.backend.Run(ctx, p.ID, p.Workspace, p.Args)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		out = json.RawMessage("null")
	}
	return out, nil
}

func (s *server) remoteCommands(ctx context.Context) ([]string, error) {
	all, err := s.backend.Commands(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(all))
	for _, c := range all {
		if c.Remote && c.Running {
			out = append(out, c.ID)
		}
	}
	return out, nil
}

func (e *apiError) Error() string {
	return e.Code + ": " + e.Message
}

func validDevice(d deviceInfo) bool {
	return d.ID != "" && len(d.ID) <= maxDeviceField && len(d.Name) <= maxDeviceField && len(d.Model) <= maxDeviceField
}

func remoteHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func decodeParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return &apiError{Code: codeInvalidParams, Message: err.Error()}
	}
	return nil
}

func toAPIError(err error) apiError {
	var api *apiError
	if errors.As(err, &api) {
		return *api
	}
	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		code := codeFailed
		if rpcErr.Code == jsonrpcInvalid {
			code = codeInvalidParams
		}
		return apiError{Code: code, Message: rpcErr.Message}
	}
	return apiError{Code: codeFailed, Message: err.Error()}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
