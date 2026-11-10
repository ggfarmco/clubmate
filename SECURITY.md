# Security

## Reporting a vulnerability

Report it privately through GitHub: [Security → Report a vulnerability](https://github.com/ggfarmco/clubmate/security/advisories/new) on this repository. Please don't open a public issue, pull request or discussion for it, and don't send it as an in-app problem report (those go to the developers' relay and are read by agents).

Include what an attacker needs (an agent session, a paired phone, a web page, a relay token, a process on the Mac), the steps, and what they get. You'll get an answer in the advisory thread; a fix lands in a release and the advisory is published with it.

Only the latest release gets fixes. Clubmate ships often, so update before you report.

## Trust model

Clubmate runs agents with your user's rights on your Mac. What it guards, and what it doesn't:

| Actor | Treated as | Where it's written down |
|---|---|---|
| You, in the app, the CLI outside an agent session, or a paired phone | the user | [Agents § Approvals](docs/agents.md#approvals), [Remote § Security model](docs/remote.md#security-model) |
| An agent using the MCP tools | limited: agent-visible commands only, destructive ones ask you, its own workspace only | below |
| Any process running as your user (an agent's shell, a plugin, a script) | the user: the socket is not a boundary | below |
| Web pages, report bundles, files in a repo | untrusted data | [Agent browser](docs/browser.md), [Remote § Problem reports](docs/remote.md#problem-reports) |
| The push relay and its callers | authenticated per Mac by a token | [Push relay](docs/push-relay.md) |

### The socket is not a boundary

The core listens on a unix socket in your runtime dir, mode `0700`, and checks that a peer has your uid. Past that it trusts what a request says about itself: `agent`, `caller` (the agent session) and `terminal` are request fields. So every guard below that looks at who is calling stops an agent from acting for you **by accident** or through the MCP tools. It does not stop a process with your uid that unsets `CLUBMATE_AGENT_SESSION`, makes up a caller or talks to the socket itself. Agents get a shell, so treat anything an agent can run as able to do what you can. Making the core verify where a call comes from is open work (backlog task 401).

### What an agent may do

- Only commands marked `agent: true` in their plugin's manifest, listed by `clubmate command list --agent` ([Agents § What agents can do](docs/agents.md#what-agents-can-do)). Hidden helpers and user-only commands are out of reach.
- A prompt an agent sends to another session is marked as not coming from you, so it never counts as your approval.
- Some actions stay yours even through plain `cm` from an agent's shell: answering connect and land requests, mode and config, AFK and finish defaults, open items, graph approval and trust, pairing and companion access. They refuse any agent session as caller. These are guards, not boundaries (see above).

### Destructive commands ask you

A command marked `destructive` shows an alert in the app before it runs: closing or killing sessions, typing into a terminal, answering a session as you, removing a worktree, and others. The alert names the action, workspace, target session, the agent session that asked and every argument. Deny is the default. With no answer within 60 s, or with the app closed, the command is refused ([Agents § Approvals](docs/agents.md#approvals)). Cron jobs, triggers and graph steps call commands as an agent, so they get the same approval ([cron](docs/cron.md), [triggers](docs/triggers.md)).

### Workspace scope

An agent acts only in its own workspace. `clubmate mcp` refuses a `workspace` argument other than `$CLUBMATE_WORKSPACE_ID`, and the core refuses an agent's `session.read`, `session.send` and `session.kill` on another workspace's session ([Agents § MCP](docs/agents.md#mcp-claude-code-and-other-mcp-clients)). Some lists (`session.list`, `process.tree`) still span all workspaces.

### Phone and remote tokens

- The Mac listens only on its Tailscale addresses. WireGuard authenticates the hosts, and a per-device token authorises the app ([Remote § Network](docs/remote.md#network)).
- Pairing codes are 128 random bits, single use, valid 5 minutes and rate-limited per source. Device tokens are 32 random bytes; the Mac stores only their SHA-256. Revoking a device closes its streams.
- A paired phone acts as you. It reads every workspace's plugin UI, presses any of its buttons, runs the commands marked `remote` and answers any pending permission ([Remote § Security model](docs/remote.md#security-model)). Pair only devices you control.
- Pushes go Mac → [push relay](docs/push-relay.md) → APNs. Each Mac has its own relay token. Once a Mac registers the device tokens of its paired phones, the relay refuses its pushes to any other device. The relay authenticates that registration with the Mac's token alone, so a leaked token is revoked on the relay with `token rm` ([Push relay § Device binding](docs/push-relay.md#device-binding)).

### Agent browser

Agents browse in a hidden WebKit browser in the app ([Agent browser](docs/browser.md)).

- Page text is untrusted. The snapshot tool says so, and agents must not follow instructions found in it.
- The core refuses local and private network addresses: `localhost`, `.local`, loopback, private, CGNAT and link-local ranges, including DNS names that resolve there. The app refuses such names and IP literals on redirects and clicks. DNS rebinding can still slip through ([Agent browser § Address guard](docs/browser.md#address-guard)).
- Each workspace has its own cookie and sign-in store. Through the MCP tools, a sign-in made for one workspace's agent during a handoff can't be used from another workspace, but any agent session of that workspace can use it ([Agent browser § Data store](docs/browser.md#data-store)). From an agent's shell the socket rule above applies.
