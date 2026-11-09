package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clubmate/internal/rpc"
)

const callTimeout = 30 * time.Second

type helper struct {
	socket    string
	workspace string
	signals   string
	agentLog  string
	sim       string
	bundle    string
	repos     string

	mu       sync.Mutex
	pushes   []push
	presence *rpc.Client
}

type push struct {
	Token       string          `json:"token"`
	Environment string          `json:"environment"`
	Type        string          `json:"type"`
	CollapseID  string          `json:"collapseID"`
	Payload     json.RawMessage `json:"payload"`
	Delivered   string          `json:"delivered"`
}

type runRequest struct {
	ID        string          `json:"id"`
	Workspace string          `json:"workspace"`
	Args      json.RawMessage `json:"args"`
	Agent     bool            `json:"agent"`
	Caller    string          `json:"caller"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	h := &helper{}
	flag.StringVar(&h.socket, "socket", "", "core socket")
	flag.StringVar(&h.workspace, "workspace", "", "default workspace id")
	flag.StringVar(&h.signals, "signals", "", "directory the fake agent waits on")
	flag.StringVar(&h.agentLog, "agent-log", "", "FAKEAGENT_LOG of the core")
	flag.StringVar(&h.sim, "sim", "", "simulator udid pushes go to")
	flag.StringVar(&h.bundle, "bundle", "com.clubmate.companion", "app bundle id")
	flag.StringVar(&h.repos, "repos", "", "directory new scratch repos go in")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /rpc", h.rpc)
	mux.HandleFunc("POST /run", h.run)
	mux.HandleFunc("POST /signal/{name}", h.signal)
	mux.HandleFunc("GET /agent-log", h.log)
	mux.HandleFunc("POST /v1/push", h.relay)
	mux.HandleFunc("GET /pushes", h.listPushes)
	mux.HandleFunc("DELETE /pushes", h.clearPushes)
	mux.HandleFunc("GET /config", h.config)
	mux.HandleFunc("POST /repo/{name}", h.repo)
	mux.HandleFunc("POST /commit", h.commit)
	mux.HandleFunc("POST /write", h.write)
	mux.HandleFunc("PUT /mac-open", h.openMac)
	mux.HandleFunc("DELETE /mac-open", h.closeMac)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func (h *helper) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	c, err := rpc.Dial(ctx, h.socket)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var out json.RawMessage
	err = c.Call(ctx, method, params, &out)
	return out, err
}

func (h *helper) rpc(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := h.call(r.Context(), req.Method, req.Params)
	reply(w, out, err)
}

func (h *helper) run(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Workspace == "" {
		req.Workspace = h.workspace
	}
	if len(req.Args) == 0 {
		req.Args = json.RawMessage(`{}`)
	}
	params := map[string]any{"id": req.ID, "workspaceId": req.Workspace, "args": req.Args, "agent": req.Agent, "caller": req.Caller}
	out, err := h.call(r.Context(), "command.run", params)
	reply(w, out, err)
}

func (h *helper) signal(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.ContainsAny(name, "/.") {
		http.Error(w, "bad signal name", http.StatusBadRequest)
		return
	}
	reply(w, json.RawMessage(`{}`), os.WriteFile(filepath.Join(h.signals, name), nil, 0o600))
}

func (h *helper) log(w http.ResponseWriter, _ *http.Request) {
	raw, err := os.ReadFile(h.agentLog)
	if os.IsNotExist(err) {
		raw, err = nil, nil
	}
	lines := []json.RawMessage{}
	for line := range bytes.Lines(raw) {
		if line = bytes.TrimSpace(line); len(line) > 0 {
			lines = append(lines, line)
		}
	}
	out, _ := json.Marshal(lines)
	reply(w, out, err)
}

func (h *helper) relay(w http.ResponseWriter, r *http.Request) {
	var p push
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.Delivered = h.deliver(r.Context(), p)
	h.mu.Lock()
	h.pushes = append(h.pushes, p)
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":200}`)
}

func (h *helper) deliver(ctx context.Context, p push) string {
	if h.sim == "" {
		return "skipped"
	}
	cmd := exec.CommandContext(ctx, "xcrun", "simctl", "push", h.sim, h.bundle, "-")
	cmd.Stdin = bytes.NewReader(p.Payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("simctl push: %v: %s", err, bytes.TrimSpace(out))
	}
	return "simctl"
}

func (h *helper) listPushes(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	out, err := json.Marshal(append([]push{}, h.pushes...))
	h.mu.Unlock()
	reply(w, out, err)
}

func (h *helper) clearPushes(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	h.pushes = nil
	h.mu.Unlock()
	reply(w, json.RawMessage(`{}`), nil)
}

func (h *helper) config(w http.ResponseWriter, _ *http.Request) {
	out, err := json.Marshal(map[string]string{"workspace": h.workspace, "signals": h.signals})
	reply(w, out, err)
}

func (h *helper) openMac(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.presence != nil {
		reply(w, json.RawMessage(`{}`), nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), callTimeout)
	defer cancel()
	c, err := rpc.Dial(context.WithoutCancel(r.Context()), h.socket)
	if err == nil {
		err = c.Call(ctx, "ui.subscribe", map[string]any{"workspaceId": h.workspace}, nil)
	}
	if err != nil {
		if c != nil {
			c.Close()
		}
		reply(w, nil, err)
		return
	}
	h.presence = c
	reply(w, json.RawMessage(`{}`), nil)
}

func (h *helper) closeMac(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.presence != nil {
		h.presence.Close()
		h.presence = nil
	}
	reply(w, json.RawMessage(`{}`), nil)
}

func (h *helper) repo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.ContainsAny(name, "/.") {
		http.Error(w, "bad repo name", http.StatusBadRequest)
		return
	}
	dir := filepath.Join(h.repos, name)
	err := os.MkdirAll(dir, 0o700)
	if err == nil {
		err = git(r.Context(), dir, "init", "-q", "-b", "main")
	}
	if err == nil {
		err = git(r.Context(), dir, "commit", "-q", "--allow-empty", "-m", "init")
	}
	out, _ := json.Marshal(map[string]string{"path": dir})
	reply(w, out, err)
}

func (h *helper) commit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir  string `json:"dir"`
		File string `json:"file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.File == "" || strings.ContainsAny(req.File, "/") {
		http.Error(w, "bad commit request", http.StatusBadRequest)
		return
	}
	err := os.WriteFile(filepath.Join(req.Dir, req.File), []byte("change\n"), 0o600)
	if err == nil {
		err = git(r.Context(), req.Dir, "add", req.File)
	}
	if err == nil {
		err = git(r.Context(), req.Dir, "commit", "-q", "-m", "change "+req.File)
	}
	reply(w, json.RawMessage(`{}`), err)
}

func (h *helper) write(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !filepath.IsAbs(req.Path) {
		http.Error(w, "bad write request", http.StatusBadRequest)
		return
	}
	reply(w, json.RawMessage(`{}`), os.WriteFile(req.Path, []byte(req.Text), 0o600))
}

func git(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "user.name=e2e", "-c", "user.email=e2e@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}

func reply(w http.ResponseWriter, result json.RawMessage, err error) {
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{"result": result}
	if err != nil {
		body = map[string]any{"error": err.Error()}
	}
	_ = json.NewEncoder(w).Encode(body)
}
