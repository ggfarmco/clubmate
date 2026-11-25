package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/tunnelrelay"
	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	maxBodyBytes      = 16 << 20
	maxDeviceField    = 128
	maxMacs           = 64
	requestTimeout    = 2 * time.Minute
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 2 * time.Minute
	maxHeaderBytes    = 64 << 10
	jsonContentType   = "application/json; charset=utf-8"
	jsonrpcInvalid    = -32602
)

const (
	codeUnauthorized  = "unauthorized"
	codePairingFailed = "pairing_failed"
	codeNotAllowed    = "not_allowed"
	codeInvalidParams = "invalid_params"
	codeFailed        = "failed"
	codeRefused       = "refused"
	codeTooLarge      = "too_large"
)

var errStreamClosed = errors.New("stream closed")

type server struct {
	backend   backend
	devices   *deviceStore
	companion *companionStore
	pairing   *pairing
	hub       *hub
	identity  *identity
	relay     *relayClient
	tun       *tunnelClient
	requests  *requestLog
	changed   func()
	watching  func(contract.RemoteWatching)
	ping      time.Duration
	now       func() time.Time
	pushed    pushLog
	presence  presence
	quiet     *quietLog

	relayAlert relayAlert

	uiMu     sync.Mutex
	sections map[string][]string

	cacheMu sync.Mutex
	gen     int64
	cmds    []string
	version string
}

type computer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Colour string `json:"colour"`
	Model  string `json:"model,omitempty"`
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
	Token       string   `json:"token"`
	Computer    computer `json:"computer"`
	RelayTicket string   `json:"relayTicket,omitempty"`
}

type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type commandRequest struct {
	ID        string          `json:"id"`
	Workspace string          `json:"workspace"`
	Args      json.RawMessage `json:"args"`
	RequestID string          `json:"requestId"`
}

type macsRequest struct {
	Macs []macInfo `json:"macs"`
}

type colourRequest struct {
	Colour string `json:"colour"`
}

type identityResult struct {
	Computer computer `json:"computer"`
}

type helloResult struct {
	Computer computer    `json:"computer"`
	Version  string      `json:"version"`
	Commands []string    `json:"commands"`
	Topics   []string    `json:"topics"`
	UI       bool        `json:"ui"`
	Requests bool        `json:"requests"`
	Reports  bool        `json:"reports"`
	Tunnel   *tunnelInfo `json:"tunnel,omitempty"`
}

type workspaceInfo struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Folders  []pluginsdk.Folder `json:"folders"`
	Roots    []string           `json:"roots"`
	Home     bool               `json:"home,omitempty"`
	Activity json.RawMessage    `json:"activity,omitempty"`
}

type deviceCtxKey struct{}

type authedHandler func(w http.ResponseWriter, r *http.Request, d device)

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pair", gzipped(s.pair))
	mux.HandleFunc("POST /v1/rpc", gzipped(s.authed(s.rpc)))
	mux.HandleFunc("GET /v1/events", s.authed(s.events))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode := pairingMode(r.Context()); {
		case mode == pairingSpent:
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "a pairing channel takes one request")
			return
		case mode == pairingOpen && (r.Method != http.MethodPost || r.URL.Path != "/v1/pair"):
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "this relay ticket is for pairing only")
			return
		}
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
		logEvent("pair", "result", "invalid")
		writeError(w, http.StatusBadRequest, codeInvalidParams, "device id, name and model must be at most 128 characters; id is required")
		return
	}
	gen, result := s.pairing.try(req.Code, remoteHost(r.RemoteAddr))
	logEvent("pair", "result", result)
	if result != pairOK {
		writeError(w, http.StatusForbidden, codePairingFailed, "the pairing code is wrong, expired or already used")
		return
	}
	token, ticket, err := s.devices.add(r.Context(), req.Device)
	if err != nil {
		s.pairing.reopen(gen)
		writeError(w, http.StatusInternalServerError, codeFailed, err.Error())
		return
	}
	s.hub.revoke(req.Device.ID)
	if _, err := s.companion.paired(r.Context()); err != nil {
		logf("end the pairing offer: %v", err)
	}
	s.changed()
	s.relayDevicesChanged(r.Context())
	_ = s.syncTickets(r.Context(), s.pairing.ticketHash())
	onReplySent(r.Context(), func() { _ = s.syncTickets(context.Background()) })
	writeJSON(w, http.StatusOK, pairResponse{Token: token, Computer: s.identity.get(), RelayTicket: ticket})
}

func (s *server) syncTickets(ctx context.Context, keep ...string) error {
	if s.tun == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, relaySyncTimeout)
	defer cancel()
	err := s.tun.syncTickets(ctx, keep...)
	if err != nil {
		logf("send the ticket set to the relay: %v", err)
	}
	return err
}

func (s *server) ticketKind(hash string) ticketKind {
	if hash == "" {
		return ticketUnknown
	}
	if s.devices.hasTicket(hash) {
		return ticketDevice
	}
	if live, ok := s.pairing.liveTicket(); ok && live.Hash == hash {
		return ticketPairing
	}
	return ticketUnknown
}

func (s *server) ticketSet(keep ...string) []tunnelrelay.Ticket {
	list := s.devices.tickets()
	room := tunnelrelay.MaxTickets - 1
	if len(list) > room {
		list = list[:room]
	}
	if live, ok := s.pairing.liveTicket(); ok {
		list = append(list, live)
	}
	for _, h := range keep {
		if h != "" && !slices.ContainsFunc(list, func(t tunnelrelay.Ticket) bool { return t.Hash == h }) {
			list = append(list, tunnelrelay.Ticket{Hash: h, Exp: s.pairing.expiry().Unix()})
		}
	}
	return list
}

func (s *server) revoke(ctx context.Context, id string) error {
	if err := s.devices.revoke(ctx, id); err != nil {
		return err
	}
	s.hub.revoke(id)
	s.changed()
	s.relayDevicesChanged(ctx)
	_ = s.syncTickets(ctx)
	return nil
}

func (s *server) authed(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			logEvent("auth", "device", "-", "path", r.URL.Path, "err", "missing")
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "missing device token")
			return
		}
		d, ok := s.devices.authenticate(r.Context(), token)
		if !ok {
			logEvent("auth", "device", "-", "path", r.URL.Path, "err", "unknown")
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "unknown or revoked device token")
			return
		}
		next(w, r.WithContext(withDevice(r.Context(), d)), d)
	}
}

func (s *server) rpc(w http.ResponseWriter, r *http.Request, d device) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(requestTimeout))
	_ = rc.SetWriteDeadline(time.Now().Add(requestTimeout))
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api := &apiError{Code: codeInvalidParams, Message: "invalid request body"}
		s.logRPC(d, rpcRequest{Method: "-"}, 0, api)
		writeJSON(w, http.StatusOK, errorBody{Error: *api})
		return
	}
	started := s.now()
	result, err := s.call(r.Context(), d, req)
	s.logRPC(d, req, s.now().Sub(started), err)
	if err != nil {
		writeJSON(w, http.StatusOK, errorBody{Error: toAPIError(err)})
		return
	}
	writeJSON(w, http.StatusOK, resultBody{Result: result})
}

func (s *server) call(ctx context.Context, d device, req rpcRequest) (any, error) {
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
	case "macs":
		var p macsRequest
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, s.macs(ctx, d, p.Macs)
	case "setIdentityColour":
		var p colourRequest
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return s.setIdentityColour(ctx, p.Colour)
	case "pushToken":
		var p pushTokenRequest
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return s.pushToken(ctx, d, p)
	case "uiSections":
		return s.uiSections(ctx)
	case "uiGet":
		var p uiGetRequest
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return s.uiGet(ctx, p)
	case "uiEvent":
		var p uiEventRequest
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return s.uiEvent(ctx, d, p)
	case "uiWatch":
		var p uiWatchRequest
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return nil, s.uiWatch(ctx, d, p)
	case "report":
		var p phoneReportRequest
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return s.report(ctx, d, p)
	}
	return nil, &apiError{Code: codeNotAllowed, Message: "unknown method: " + req.Method}
}

func (s *server) hello(ctx context.Context) (helloResult, error) {
	cmds, err := s.remoteCommands(ctx)
	if err != nil {
		return helloResult{}, err
	}
	version, err := s.coreVersion(ctx)
	if err != nil {
		return helloResult{}, err
	}
	res := helloResult{Computer: s.identity.get(), Version: version, Commands: cmds, Topics: slices.Clone(forwardedTopics), UI: true, Requests: true, Reports: true}
	if s.tun != nil {
		res.Tunnel = s.tun.info()
	}
	return res, nil
}

func (s *server) workspaces(ctx context.Context) ([]workspaceInfo, error) {
	workspaces, err := s.backend.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	activity := s.activity(ctx)
	out := make([]workspaceInfo, 0, len(workspaces))
	for _, p := range workspaces {
		folders := p.Folders
		if folders == nil {
			folders = []pluginsdk.Folder{}
		}
		out = append(out, workspaceInfo{ID: p.ID, Name: p.Name, Folders: folders, Roots: p.Roots(), Home: p.Home, Activity: activity[p.ID]})
	}
	return out, nil
}

func (s *server) activity(ctx context.Context) map[string]json.RawMessage {
	raw, err := s.backend.Run(ctx, activityCommand, "", nil)
	if err != nil {
		return nil
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func (s *server) command(ctx context.Context, p commandRequest) (json.RawMessage, error) {
	cmds, err := s.remoteCommands(ctx)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(cmds, p.ID) {
		return nil, &apiError{Code: codeNotAllowed, Message: "command is not available remotely: " + p.ID}
	}
	if p.RequestID == "" {
		return s.run(ctx, p)
	}
	if !validRequestID(p.RequestID) {
		return nil, &apiError{Code: codeInvalidParams, Message: "requestId must be at most 64 letters, digits, '-' or '_'"}
	}
	return s.requests.do(ctx, p.RequestID, func(ctx context.Context) (json.RawMessage, error) {
		return s.run(ctx, p)
	})
}

func (s *server) run(ctx context.Context, p commandRequest) (json.RawMessage, error) {
	out, err := s.backend.Run(ctx, p.ID, p.Workspace, p.Args)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		out = json.RawMessage("null")
	}
	return out, nil
}

func (s *server) macs(ctx context.Context, d device, macs []macInfo) error {
	if !validMacs(macs) {
		return &apiError{Code: codeInvalidParams, Message: "at most 64 macs, names and colours at most 128 characters"}
	}
	changed, err := s.devices.setMacs(ctx, d.ID, macs)
	if err != nil {
		return err
	}
	if _, err := s.identity.ensureColour(ctx, s.devices.usedColours()); err != nil {
		return err
	}
	if changed {
		s.changed()
	}
	return nil
}

func (s *server) setIdentityColour(ctx context.Context, colour string) (identityResult, error) {
	c, err := s.identity.setColour(ctx, colour)
	if errors.Is(err, errBadColour) {
		return identityResult{}, &apiError{Code: codeInvalidParams, Message: err.Error()}
	}
	if err != nil {
		return identityResult{}, err
	}
	return identityResult{Computer: c}, nil
}

func (s *server) remoteCommands(ctx context.Context) ([]string, error) {
	s.cacheMu.Lock()
	cached, gen := s.cmds, s.gen
	s.cacheMu.Unlock()
	if cached != nil {
		return cached, nil
	}
	all, err := s.backend.Commands(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(all))
	for _, c := range all {
		if c.Remote && c.Running {
			out = append(out, c.ID)
			out = append(out, c.Aliases...)
		}
	}
	s.cacheMu.Lock()
	if s.gen == gen {
		s.cmds = out
	}
	s.cacheMu.Unlock()
	return out, nil
}

func (s *server) forgetCommands() {
	s.cacheMu.Lock()
	s.gen++
	s.cmds = nil
	s.cacheMu.Unlock()
}

func (s *server) coreVersion(ctx context.Context) (string, error) {
	s.cacheMu.Lock()
	version := s.version
	s.cacheMu.Unlock()
	if version != "" {
		return version, nil
	}
	version, err := s.backend.Version(ctx)
	if err != nil {
		return "", err
	}
	s.cacheMu.Lock()
	s.version = version
	s.cacheMu.Unlock()
	return version, nil
}

func (s *server) logRPC(d device, req rpcRequest, took time.Duration, err error) {
	if err == nil && took < slowRPC {
		return
	}
	kv := []string{"device", logID(d.ID), "method", req.Method}
	if req.Method == "command" {
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(req.Params, &p)
		kv = append(kv, "id", p.ID)
	}
	kv = append(kv, "ms", logMillis(took))
	if err != nil {
		kv = append(kv, "err", toAPIError(err).Code)
	}
	logEvent("rpc", kv...)
}

func withDevice(ctx context.Context, d device) context.Context {
	return context.WithValue(ctx, deviceCtxKey{}, d.ID)
}

func logDevice(ctx context.Context) string {
	id, _ := ctx.Value(deviceCtxKey{}).(string)
	return logID(id)
}

func (e *apiError) Error() string {
	return e.Code + ": " + e.Message
}

func validDevice(d deviceInfo) bool {
	return d.ID != "" && len(d.ID) <= maxDeviceField && len(d.Name) <= maxDeviceField && len(d.Model) <= maxDeviceField
}

func validMacs(macs []macInfo) bool {
	return len(macs) <= maxMacs && !slices.ContainsFunc(macs, func(m macInfo) bool {
		return len(m.Name) > maxDeviceField || len(m.Colour) > maxDeviceField
	})
}

func remoteHost(addr string) string {
	if strings.HasPrefix(addr, relayAddrPrefix) {
		return addr
	}
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
		if reason := contract.ReasonIn(rpcErr.Data); reason != "" {
			code = reason
		} else if pluginRefusal(err) {
			code = codeRefused
		}
		if pe, ok := pluginsdk.PluginErrorOf(err); ok {
			return apiError{Code: code, Message: pe.Message}
		}
		return apiError{Code: code, Message: rpcErr.Message}
	}
	return apiError{Code: codeFailed, Message: err.Error()}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
