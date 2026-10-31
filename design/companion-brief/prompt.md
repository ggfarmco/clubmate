# Clubmate Companion — design brief

Paste this into the same Claude Design project as the Mac design language. Attach `screens/*.png` and, from `design/language/png/`, `Principles.png`, `Tokens.png`, `AgentNodes.png` and `Agents-640.png`.

## 1. Your task

Clubmate now has a design language for the Mac app (the attached boards). Extend it to the iPhone/iPad companion so both read as one product:

1. **Audit** the companion screenshots: problems per screen, ranked by impact, with special weight on anything that lets a tap do something the user did not mean.
2. **Companion adaptation of the language**: what carries over unchanged (principles, colour roles, the node specs for messages, plan, tool groups, permission requests, composer), what changes on a phone (touch targets, type scale, density, navigation, gestures, one-handed reach), and the accent/brand decision below.
3. **Mockups** at iPhone (393×852) dark and light: Macs list, a workspace's agent sessions, a session waiting for permission, a session asking a question, the inbox. One iPad layout (split view) for the session screen.
4. **Hand-off** per change: (a) companion-only SwiftUI, (b) needs data the companion does not get today (say what), (c) shared with the Mac renderers.

## 2. What the companion is

A native SwiftUI app (iOS 26+/iPadOS) that drives Claude agents running on the user's paired Macs, over Tailscale, through the Mac's `remote` plugin. It lists the Macs, each Mac's workspaces and agent sessions, shows a session's transcript, answers permission requests and questions, sends prompts (text, photos, video frames), starts and stops sessions, and shows the Mac's notification inbox. It is used away from the desk, in short bursts, usually to unblock an agent.

The core jobs, in order: see what needs me → understand it in seconds → answer it safely → go back to my day.

## 3. Constraints

- **No Ghostty theme on the phone.** The Mac takes its colours from the user's terminal theme, so the Mate brand green stays out of the Mac window. The companion has no such theme: it follows the system appearance. The Mac board "Where the mate green belongs" lists a fallback Mate theme (dark `#151B17` / light `#F6F1E6`, accent `#A6D85B` / `#3E8A2E`, success teal so it never reads as the accent). Decide whether the companion's accent becomes Mate green and how the tones map; today it is a system indigo (`#686BF2` light / `#8D8EFF` dark).
- **Native first.** System navigation (NavigationSplitView, large titles, Liquid Glass toolbars), SF Symbols, Dynamic Type, VoiceOver. Don't port the Mac's monospace-everything look; code, paths and commands are monospace, prose is the system font.
- **Same data as the Mac**: sessions with status (running, waiting for permission, done, asking, error, closed), child sessions under their orchestrator, AFK mode, transcript entries (user, agent, thought, tool calls with diffs, plan, permission requests with options of kind allow_once / allow_always / reject_once / reject_always), notifications (needs-input, permission, done, stuck, info).
- **Safety rule from the Mac**: a permanent permission ("Always allow") must never be the default or most prominent choice; one-time Allow comes first.

## 4. Screenshots

All from the simulator with fixture data (`-fixtures`, see the README), iPhone 18 Pro, dark and light.

| File | Shows | What I see |
|---|---|---|
| `01-macs-*` | Paired Macs, each with Agents and Inbox counts | Plain grouped list; the online dot is the only state; counts don't say what needs me. |
| `02-agents-*` | One workspace's sessions grouped "Needs Input" / "Closed" | Readable; the orange "Permission" pill and hand icon say the same thing twice; child count `⑂ 1` is cryptic. |
| `03-session-permission-*` | Transcript: agent message with a code block, plan card, tool rows, permission request, composer | **"Always Allow" is first and the most prominent (filled green)** — the opposite of the Mac rule. The plan appears twice (markdown "Plan" and the plan card); done steps are struck through. The glass header covers the start of the transcript. Every tool call is its own card. |
| `04-session-question-*` | A child session in AFK mode that ended with a question | The question is just the last line of text; nothing marks it as waiting for an answer. User bubble in indigo. |
| `05-inbox-*` | Notifications with kind icons, time, workspace, unread dot | Close to the Mac spec already; no grouping into "Waiting for you" / "Earlier". |

## 5. What to deliver

1. Audit table (screen · problem · why · fix).
2. The adaptation sheet: carried-over principles, phone deltas (touch target ≥ 44pt, type scale on Dynamic Type, spacing, radius), colour roles with the accent decision, light and dark.
3. Component specs for the phone: session row, status badge, message, thought, plan, tool group, diff, permission request (ordering and confirmation for "always"), question-waiting state, composer (attachments, queue vs send while running, stop), inbox row, empty and offline states.
4. Mockups listed in §1.
5. Hand-off table and open questions.
