# Voice

Press the mic or hold ⌥Space on the Mac (the buttons come with the Mac and phone slices; today the commands below drive it), speak, and Clubmate's Voice assistant answers the agent you are looking at or runs the command you said. The text shows up while you speak. Dangerous actions wait for your Yes on the device you spoke from.

The `voice` plugin (`plugins/voice`) owns utterances. A Python recognizer (`plugins/voice/asr`) turns speech into text on the Mac. One Voice agent session per workspace decides what to do.

## Flow
1. The client calls `voice.start` with a pointer to where the user is. The plugin resolves the pointer into context (below) before recognition starts, and passes words from it to the recognizer as a hint.
2. The client streams audio with `voice.audio`; every call returns the partial text so far.
3. `voice.end` returns `heard`: the final text with the user dictionary applied. Partials are raw; the dictionary applies to `heard` only.
4. The plugin prompts the workspace's Voice session with `{utterance, device, heard, context}`. The session calls `voice.plan`, acts, and always ends with `voice.done`.
5. The client follows `voice.changed` and reads `voice.state` to show the plan, a confirmation and the result.

## Commands
All are hidden. The client commands are `remote: true`, so the phone calls them through the `remote` plugin.

| Command | Args | Result |
|---|---|---|
| `voice.start` | `{pointer: {device: "mac"\|"phone", section, selection: {session?, task?, file?, line?}}, lang?: "ru"\|"auto"}` (default `ru`) | `{utterance: "vu_…"}`; phase `loading` while the model loads (audio is kept, at most 60 s), else `listening`. Fails with `voice needs uv (https://docs.astral.sh/uv/)` when `uv` is missing. |
| `voice.audio` | `{utterance, pcm}`: base64 PCM s16le, 16 kHz mono; 320 ms chunks (10 240 bytes) recommended, at most 64 KiB raw per call | `{partial}`. After 60 s of audio the rest is cut and the utterance ends itself. `unknown utterance` once it ended or was cancelled. |
| `voice.end` | `{utterance}` | `{heard}`; routing goes on in the background. |
| `voice.cancel` | `{utterance}` | `null`; a pending confirmation is declined. |
| `voice.state` | `{utterance}` | `State` |
| `voice.confirm` | `{utterance, approve}` | `null`; `false` ends with result `cancelled`. |
| `voice.dictionary` (app) | – | `{entries: [{from, to}]}` |
| `voice.dictionary-set` (app) | `{entries}` | the stored dictionary. Matching is whole-word, case-insensitive, longest `from` first. |

`State` = `{utterance, phase, partial?, heard?, plan?, result?, error?, confirm?: {title, command, args}, session?, rev}`. `phase` is `loading`, `listening`, `thinking`, `confirm`, `done`, `error` or `cancelled`. `session` is the Voice session's id, so a client can open its transcript. Every change publishes `voice.changed {workspace, utterance, rev}`, and the `remote` plugin forwards it to the phone.

## Context
The pointer carries ids only; the plugin resolves them:
- **Session:** title, status, the pending question (`fields` with options) and the pending permission request (options with `kind`), from `agents.sessions` and `agents.transcript`. A plain question in the agent's last message comes through as `{text}`.
- **Task:** the backlog task.
- **View:** the section's `<section>.main` SDUI tree from `ui.get`, or `{"truncated": true}` when over 8 KiB.
- **Workspace:** name, branches, sessions; `needsYou` lists the sessions waiting for the user.
- **No selection:** in Agents and Backlog the plugin reads the `selected` value of `<section>.sidebar`. With nothing selected in Agents, it takes the session that has needed the user longest.

## The Voice session
- **What it is:** one per workspace, titled "Voice", created on first use (its id is in plugin storage). It is reused while `agents.sessions` lists it, and recreated once when it cannot be prompted.
- **How it is set up:** the plugin creates it with `mode: bypassPermissions`, notifications muted, its own system prompt (`plugins/voice/prompt.md`), and `disallowedTools` = the built-in tools plus the MCP tools of every dangerous command. It has no shell and no file tools, and it is not selected in the Agents sidebar.
- **What it sees:** Clubmate adds no turn texts (finish mode, orchestrator, AFK) to its prompts. See [agents](agents.md).
- **Who may call its commands:** `voice.plan`, `voice.answer`, `voice.run` and `voice.done` accept only that session as caller.
  - `voice.answer`: the plugin runs `agents.answer`, `agents.elicitation` or `agents.permission` itself; `agents` treats `via: voice` as the user. Allowing a permission other than the one in the utterance's context, or "always allow", asks for confirmation first.
  - `voice.run`: only for dangerous commands. Others fail with `not dangerous`, and non-agent commands with `not an agent command`. Before running, it waits up to 60 s in phase `confirm` for `voice.confirm`.
- **Dangerous:** a command with `destructive: true` in its manifest (except `agents.answer`), plus terminal input `session.send`.
- **Limits:** a turn that does not reach `voice.done` within 90 s ends with error `the assistant did not answer`; the timer pauses during a confirmation. Commands that a plugin adds after the Voice session was created stay callable as direct tools until the session is recreated; a direct destructive call still gets core's approval on the Mac.

## Recognizer
- **Process:** `uv run --script plugins/voice/asr/server.py --engine nemotron|whisper|fake [--model …] [--chunk-ms 320]`. One long-lived process, spawned in the background when the plugin starts and respawned when it dies after it was ready. `uv` is looked up on PATH, then `~/.local/bin/uv`, then `/opt/homebrew/bin/uv`.
- **Overrides:** `CLUBMATE_VOICE_ASR_DIR` (folder with `server.py`), `CLUBMATE_VOICE_ENGINE`.
- **Engines:**
  - `nemotron` (default): `nvidia/nemotron-3.5-asr-streaming-0.6b`, real streaming. The encoder runs on the GPU, decoding on the CPU. 320 ms chunks, partials ~35 ms after a chunk, final ~65 ms after end. Takes no hint.
  - `whisper` (batch): `mlx-community/whisper-large-v3-turbo`. Partials come from re-decoding the audio so far, at most once a second. Takes the hint as `initial_prompt`.
- **Protocol:** JSON lines over stdin/stdout.
  - In: `{"op":"start","id","lang","hint"}`, `{"op":"audio","id","pcm"}`, `{"op":"end","id"}`, `{"op":"cancel","id"}`.
  - Out: `{"ev":"ready","engine","streaming"}`, `{"ev":"partial","id","text"}`, `{"ev":"final","id","text","ms"}`, `{"ev":"error","id","message"}`.
- **Startup:** loading takes 10–25 s with the model cached; the first download takes minutes.
- **Measuring:** `plugins/voice/asr/smoke.py` measures partial latency again. `scripts/voicebench/` compares models on recorded phrases.
