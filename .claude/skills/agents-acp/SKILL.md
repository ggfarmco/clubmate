---
name: agents-acp
description: Use when designing or building Clubmate's Agents features — the ACP (Agent Client Protocol) client, hosting Claude Code / Codex / Gemini as agents, agent session history, checkpoints, handoffs, a mobile companion, or agent orchestration. Distilled from the 2026-10-29 research on ACP, Zed's agent client, DeltaDB and comparable systems.
---

# Agents in Clubmate: ACP and session history

Full research, with sources and dates, lives in the private notes repo:
- `~/develop/norimori/ggfarmco/clubmate-notes/research/2026-10-29-acp.md`
- `~/develop/norimori/ggfarmco/clubmate-notes/research/2026-10-29-history-handoff.md`

Re-check anything marked volatile there before relying on it; ACP and the adapters change monthly.

## The protocol in one screen
- **Wire:** JSON-RPC 2.0 over the agent's stdio, one message per line. The client (Clubmate) spawns the agent.
- **Versions:** v1 is stable (schema v1.27.0, 2026-10-27). v2 is a draft. Speak v1 on the wire, but shape the internal model like v2: upserts keyed by type and id, an explicit idle/running state, subagent sessions.
- **Agent → client:** `session/update` with message and thought chunks, `tool_call` and its updates (status, diffs, terminals, file locations), `plan`, available commands, mode changes. Also `session/request_permission`, and optionally `fs/*` and `terminal/*`.
- **Client → agent:** `initialize`, `authenticate`, `session/new` (cwd + MCP servers), `session/prompt` (text, resources, images), `session/cancel`, and capability-gated `session/list`, `resume`, `load`, `close`, `delete`. `fork` is unstable.
- **v2 removes `fs/*`, `terminal/*` and modes.** Editor-aware tools then reach the agent only as an MCP server passed at `session/new`, so `clubmate mcp` is the long-term integration.

## Agents
- **Claude Code:** `npx @agentclientprotocol/claude-agent-acp` (Node ≥ 22).
  - Uses the user's Claude login or `ANTHROPIC_API_KEY`.
  - Hooks, plugins, skills, CLAUDE.md, MCP, plan mode and permission modes work. `/clear`, `/cost`, `/login` and `/plugin` do not.
  - It writes to disk itself and ignores client `fs/*` and `terminal/*`.
  - Offering claude.ai login inside a distributed product needs Anthropic's approval; for personal use it is fine.
- **Codex:** `npx @agentclientprotocol/codex-acp`.
- **Gemini:** `gemini --acp`.
- **Others:** the ACP registry (`registry.json`) lists launch commands and checksums for ~40 more.

## Decisions to start from
1. **The ACP client lives in an `agents` plugin** (user decision 2026-10-29: "это тоже отдельный плагин"), like every other feature. It spawns agents, writes the event log to the bus and renders the Agents section. SwiftUI and the future phone app never speak ACP; they consume the plugin's normalized events and slots.
2. **Treat every agent as "writes to disk".**
   - After file changes, run `:checktime` in nvim.
   - Review comes from the git diff against the turn's checkpoint (diffview in nvim, `editor-nvim.diffview`), not from `fs/write_text_file`.
3. **One agent process per (agent, workspace, worktree).** This isolates crashes, env and auth, and fits per-worktree workspaces.
4. **Clubmate keeps its own event log as well as relying on the agent's.** Store ACP `session/update` payloads verbatim on the SQLite bus, in an envelope `{workspace, worktree, session, parent_session?, turn_seq, seq, actor, ts}`.
   - This is the shape Zed's `delta-claude` uses.
   - It feeds search, handoffs, the phone view and attribution.
5. **A turn is the unit of history.**
   - Before each `session/prompt`, take a git-object checkpoint: temporary index → `write-tree` → `commit-tree`, pinned at `refs/clubmate/cp/<session>/<turn>`.
   - This catches Bash, subagent and user edits, which agent file snapshots miss.
6. **Attribution** is per turn from the checkpoint diffs, minus user nvim edits. At commit time export it as Agent Trace records or git notes. **Defer CRDTs and DeltaDB-style per-character history** until live multi-writer editing becomes a goal.
7. **Mobile:** the agent keeps running on the Mac.
   - The Mac makes outbound-only connections to an E2E-encrypted relay; the phone is a thin client for prompts, permission answers, diffs and pushes.
   - This mirrors Claude Remote Control, Codex Remote and Happy.

## Go core pitfalls (all hit by others)
- **Line size:** never use `bufio.Scanner` with the 64 KB default; single messages reach tens of MB.
- **Reading:** keep one always-on stdout reader per connection and drain stderr into a capped log. A client that stops reading stalls the Claude adapter, and updates arrive outside prompts too.
- **Inbound requests** (permissions, `fs`) are dispatched on goroutines while `session/prompt` is pending, never serially on the reader.
- **Process lifecycle:**
  - Spawn with `Setpgid`.
  - Shutdown: close stdin, wait, SIGTERM, then SIGKILL the group.
  - Call `session/close` on dropped sessions; orphaned trees leak for days.
- **Environment:** a GUI-spawned agent has no shell `PATH` and no macOS security session. Resolve the login-shell env once and pass it, blank leaked `ANTHROPIC_API_KEY` unless intended, and test `gh` inside an agent early (TLS -26276 issue).
- **Noise:** skip unparseable stdout lines instead of dropping the connection.
- **Auth:** model the connection as `initialized → needs_auth → ready`. Terminal auth needs a real TTY (open it in a ghostty pane), then re-`initialize`.
- **Cancel:** answer pending permissions with `cancelled`, expect updates after cancel, and treat permission requests outside a prompt as valid (background subagents).
- **MCP passed per session may not reach Claude** (adapter issue #883). Fallback: register `clubmate mcp` in `.mcp.json`.

## Libraries
- **Go:** `caelis-labs/acp-go-sdk` is current with schema v1.27 but has a tiny community; `coder/acp-go-sdk` is popular but behind the schema. ACP is small and churns, so owning a ~500-line transport generated from `schema/v1/schema.json` is reasonable.
- **Swift:** only relevant if the phone ever talks to agents directly.
- **Prior art:** Aizen, a native macOS app for worktrees plus agents (`wiedymi/swift-acp`).

## Zed patterns worth copying
- Inline permission cards on the tool call.
- "Follow the agent" from tool-call file locations: `editor.open {path, line}`.
- Terminal tool calls shown inline.
- Archiving a worktree together with its threads (WIP commits + ref, then remove).
- A raw-lines debug log per connection.
