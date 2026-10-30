package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
)

const (
	agentClaude    = "claude"
	claudePackage  = "@agentclientprotocol/claude-agent-acp"
	claudeVersion  = "0.99.1"
	claudeBin      = "claude-agent-acp"
	claudeDir      = "claude-acp"
	minNodeMajor   = 22
	commandEnv     = "CLUBMATE_AGENTS_COMMAND"
	apiKeyEnv      = "ANTHROPIC_API_KEY"
	installTimeout = 10 * time.Minute
	probeTimeout   = 15 * time.Second
	mcpName        = "clubmate"
	projectEnv     = "CLUBMATE_PROJECT_ID"
	installMarker  = ".clubmate-installed"
)

var errUnknownAgent = errors.New("unknown agent")

func launcherFor(ctx context.Context, stateDir, agent, workspace, root string, installing func(bool)) (launch, error) {
	if agent != agentClaude {
		return launch{}, fmt.Errorf("%w: %s", errUnknownAgent, agent)
	}
	env := agentEnv(workspace)
	if cmd := os.Getenv(commandEnv); cmd != "" {
		return launch{argv: []string{cmd}, env: env, mcp: []acp.McpServer{}}, nil
	}
	node, err := nodeBinary(ctx)
	if err != nil {
		return launch{}, err
	}
	ictx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	script, err := installClaude(ictx, filepath.Join(stateDir, claudeDir), env, installing)
	if err != nil {
		return launch{}, err
	}
	return launch{argv: []string{node, script}, env: env, mcp: clubmateMCP(ctx, workspace, root)}, nil
}

func agentEnv(workspace string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, apiKeyEnv+"=") && !strings.HasPrefix(kv, projectEnv+"=") {
			out = append(out, kv)
		}
	}
	if workspace != "" {
		out = append(out, projectEnv+"="+workspace)
	}
	return out
}

func nodeBinary(ctx context.Context) (string, error) {
	node, err := exec.LookPath("node")
	if err != nil {
		return "", nodeError("none")
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, "--version").Output()
	version := strings.TrimSpace(string(out))
	if err != nil || nodeMajor(version) < minNodeMajor {
		return "", nodeError(version)
	}
	return node, nil
}

func nodeMajor(version string) int {
	major, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

type nodeVersionError struct {
	found string
}

func (e nodeVersionError) Error() string {
	return fmt.Sprintf("Node %d+ is required to run Claude (found %s)", minNodeMajor, e.found)
}

func nodeError(found string) error {
	if found == "" {
		found = "none"
	}
	return nodeVersionError{found: found}
}

func installClaude(ctx context.Context, prefix string, env []string, installing func(bool)) (string, error) {
	if err := os.MkdirAll(filepath.Dir(prefix), 0o700); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(prefix+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock %s: %w", prefix, err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	pkgDir := filepath.Join(prefix, "node_modules", filepath.FromSlash(claudePackage))
	if !installed(prefix, pkgDir) {
		installing(true)
		err := reinstall(ctx, prefix, env)
		installing(false)
		if err != nil {
			return "", err
		}
	}
	manifest, err := readPackage(pkgDir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", claudePackage, err)
	}
	bin := manifest.bin()
	if bin == "" {
		return "", fmt.Errorf("%s has no %s bin", claudePackage, claudeBin)
	}
	return filepath.Join(pkgDir, filepath.FromSlash(bin)), nil
}

func installed(prefix, pkgDir string) bool {
	marker, err := os.ReadFile(filepath.Join(prefix, installMarker))
	if err != nil || string(marker) != claudeVersion {
		return false
	}
	manifest, err := readPackage(pkgDir)
	return err == nil && manifest.Version == claudeVersion
}

func reinstall(ctx context.Context, prefix string, env []string) error {
	if err := os.RemoveAll(prefix); err != nil {
		return err
	}
	if err := os.MkdirAll(prefix, 0o700); err != nil {
		return err
	}
	if err := npmInstall(ctx, prefix, env); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(prefix, installMarker), []byte(claudeVersion), 0o600)
}

func npmInstall(ctx context.Context, prefix string, env []string) error {
	npm, err := exec.LookPath("npm")
	if err != nil {
		return errors.New("npm is required to install the Claude agent")
	}
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, npm, "install", "--prefix", prefix, "--no-audit", "--no-fund", claudePackage+"@"+claudeVersion)
	cmd.Dir = prefix
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if len(text) > stderrTailCap {
			text = text[len(text)-stderrTailCap:]
		}
		return fmt.Errorf("npm install %s@%s: %w\n%s", claudePackage, claudeVersion, err, text)
	}
	return nil
}

type packageManifest struct {
	Version string          `json:"version"`
	Bin     json.RawMessage `json:"bin"`
}

func readPackage(dir string) (packageManifest, error) {
	var m packageManifest
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(data, &m)
}

func (m packageManifest) bin() string {
	var single string
	if json.Unmarshal(m.Bin, &single) == nil {
		return single
	}
	var named map[string]string
	if json.Unmarshal(m.Bin, &named) != nil {
		return ""
	}
	if b, ok := named[claudeBin]; ok {
		return b
	}
	if len(named) == 1 {
		for _, b := range named {
			return b
		}
	}
	return ""
}

func clubmateMCP(ctx context.Context, workspace, root string) []acp.McpServer {
	exe, err := os.Executable()
	if err != nil {
		return []acp.McpServer{}
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return []acp.McpServer{}
	}
	cli := filepath.Join(filepath.Dir(exe), "..", "..", "clubmate")
	if info, err := os.Stat(cli); err != nil || info.Mode().Perm()&0o111 == 0 {
		return []acp.McpServer{}
	}
	if registered(ctx, root) {
		return []acp.McpServer{}
	}
	return []acp.McpServer{{Name: mcpName, Command: cli, Args: []string{"mcp"}, Env: []acp.EnvVariable{{Name: projectEnv, Value: workspace}}}}
}

func registered(ctx context.Context, root string) bool {
	claude, err := exec.LookPath("claude")
	if err != nil {
		return false
	}
	name := mcpName + "-dev"
	if pluginsdk.Flavor() == "release" {
		name = mcpName
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, claude, "mcp", "get", name)
	cmd.Dir = root
	cmd.Env = agentEnv("")
	return cmd.Run() == nil
}
