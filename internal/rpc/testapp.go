package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/sdui"
)

const (
	testAppPrefix      = "clubmate-test-"
	testAppDialTimeout = 2 * time.Second
)

var (
	testAppName    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	testAppMethods = []string{"command.list", "command.run", "project.list", "ui.event", "ui.get", "ui.key", "ui.present", "ui.snapshot", "workspace.open"}
)

type testAppCallParams struct {
	Instance string          `json:"instance"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params"`
}

type testAppInstance struct {
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
}

func testAppMethodList() []any {
	out := make([]any, len(testAppMethods))
	for i, m := range testAppMethods {
		out[i] = m
	}
	return out
}

func (s *Server) testAppMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"testapp.call": method(s.testAppCall),
		"testapp.list": method(s.testAppList),
	}
}

func (s *Server) testAppCall(ctx context.Context, p testAppCallParams) (json.RawMessage, error) {
	if !testAppName.MatchString(p.Instance) || strings.Contains(p.Instance, "..") {
		return nil, invalidParams(fmt.Sprintf("invalid instance name %q", p.Instance))
	}
	if !slices.Contains(testAppMethods, p.Method) {
		return nil, invalidParams(fmt.Sprintf("method %q is not allowed; allowed: %s", p.Method, strings.Join(testAppMethods, ", ")))
	}
	params, err := s.testAppParams(p)
	if err != nil {
		return nil, err
	}
	c, err := s.dialTestApp(ctx, p.Instance)
	if err != nil {
		return nil, &jsonrpc2.Error{Code: codeNotFound, Message: fmt.Sprintf("instance %s is not running", p.Instance)}
	}
	defer c.Close()
	var res json.RawMessage
	if err := c.Call(ctx, p.Method, params, &res); err != nil {
		var remote *RemoteError
		if errors.As(err, &remote) {
			return nil, &jsonrpc2.Error{Code: int64(remote.Code), Message: remote.Message}
		}
		return nil, err
	}
	return res, nil
}

func (s *Server) testAppParams(p testAppCallParams) (map[string]json.RawMessage, error) {
	params := map[string]json.RawMessage{}
	if len(p.Params) > 0 && string(p.Params) != "null" {
		if err := json.Unmarshal(p.Params, &params); err != nil {
			return nil, invalidParams("params: want object")
		}
	}
	switch p.Method {
	case "command.run", "ui.event":
		params["agent"] = json.RawMessage("true")
	case handleUISnapshot:
		dir := filepath.Join(s.testAppRoot, testAppPrefix+p.Instance)
		out := filepath.Join(dir, "snapshots")
		if raw, ok := params["out"]; ok {
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, invalidParams("params.out: want string")
			}
			out = filepath.Clean(out)
			if !strings.HasPrefix(out, dir+string(filepath.Separator)) {
				return nil, invalidParams(fmt.Sprintf("out must be inside %s", dir))
			}
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		params["out"] = encoded
	}
	return params, nil
}

func (s *Server) testAppList(ctx context.Context, _ empty) ([]testAppInstance, error) {
	dirs, err := filepath.Glob(filepath.Join(s.testAppRoot, testAppPrefix+"*"))
	if err != nil {
		return nil, err
	}
	out := []testAppInstance{}
	for _, dir := range dirs {
		name := strings.TrimPrefix(filepath.Base(dir), testAppPrefix)
		if !testAppName.MatchString(name) {
			continue
		}
		c, err := s.dialTestApp(ctx, name)
		if err != nil {
			continue
		}
		c.Close()
		out = append(out, testAppInstance{Name: name, Runtime: filepath.Join(dir, "run")})
	}
	return out, nil
}

func (s *Server) dialTestApp(ctx context.Context, name string) (*Client, error) {
	dir := filepath.Join(s.testAppRoot, testAppPrefix+name)
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	socket := filepath.Join(dir, "run", "core.sock")
	if _, err := os.Stat(socket); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, testAppDialTimeout)
	defer cancel()
	return Dial(ctx, socket)
}

func privateDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(st.Uid) != os.Getuid() || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s: not a private directory", dir)
	}
	return nil
}

func confirmGated(views []sdui.SlotView, slot, action string) bool {
	for _, v := range views {
		if v.Slot != slot {
			continue
		}
		var tree any
		if err := json.Unmarshal(v.Tree, &tree); err != nil {
			return true
		}
		gated := map[string]bool{}
		collectGated(tree, "", gated)
		return gated[action]
	}
	return true
}

func collectGated(v any, form string, gated map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		if c, _ := v["confirm"].(string); c != "" {
			if a, _ := v["action"].(string); a != "" {
				gated[a] = true
			}
		}
		props, _ := v["props"].(map[string]any)
		kind, _ := v["type"].(string)
		if kind == "form" {
			form = "submit"
			if a, _ := props["action"].(string); a != "" {
				form = a
			}
		}
		if c, _ := props["confirm"].(string); c != "" {
			gateNode(kind, v, props, form, gated)
		}
		for _, child := range v {
			collectGated(child, form, gated)
		}
	case []any:
		for _, child := range v {
			collectGated(child, form, gated)
		}
	}
}

func gateNode(kind string, node, props map[string]any, form string, gated map[string]bool) {
	if a, _ := props["action"].(string); a != "" {
		gated[a] = true
		return
	}
	id, _ := node["id"].(string)
	gated[id] = true
	switch kind {
	case "select":
		gated["select"] = true
	case "toggle":
		gated["toggle"] = true
	case "folderField":
		gated["pick"] = true
	case "textField":
		gated["submit"] = true
	}
	if form != "" && (kind == "button" || kind == "textField") {
		gated[form] = true
	}
}
