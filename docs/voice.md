# Voice

Press the mic on the Mac or the phone, or hold ⌥Space on the Mac, speak, and Clubmate's Voice assistant answers the agent you are looking at or runs the command you said. The text shows up while you speak. Dangerous actions wait for your Yes on the device you spoke from.

The `voice` plugin (`plugins/voice`) owns utterances. A Python recognizer (`plugins/voice/asr`) turns speech into text on the Mac. One Voice agent session per workspace decides what to do.

## Flow
1. The client calls `voice.start` with a pointer to where the user is. The plugin resolves the pointer into context (below) before recognition starts, and passes words from it to the recognizer as a hint.
2. The client streams audio with `voice.audio`; every call returns the partial text so far.
3. `voice.end` returns `heard`: the final text with the user dictionary applied. Partials are raw; the dictionary applies to `heard` only.
4. The plugin prompts the workspace's Voice session with `{utterance, device, heard, context}`. The session calls `voice.plan`, acts, and always ends with `voice.done`.
5. The client follows `voice.changed` and reads `voice.state` to show the plan, a confirmation and the result.

## Mac
- **Trigger:** the mic at the left of the status bar, or ⌥Space while Clubmate is frontmost. Hold to talk and release to send; a click or a tap under 250 ms starts listening and the next one sends. ⌥Space is taken before terminals, nvim and text fields.
- **Pointer:** `{device: "mac", section: <selected section>}`; the plugin finds the selection itself (see Context).
- **Panel:** at the bottom centre of the workspace window, over the content. It shows the phase, a level meter while listening with how to send ("Release to send" while ⌥Space or the mic is held, "⌥Space to send" after a tap), the partial text and then `heard`, the plan, the confirmation with Yes ↩ / No Esc, and the result or error. Open Voice session reveals the Voice session in Agents. It hides itself 4 s after `done`; an error stays until closed (×) or the next utterance. A confirmation takes key focus, so ↩ and Esc answer it only until focus moves elsewhere; ↩ is ignored for the first 0.6 s. String args show JSON-quoted, so a newline in them can't fake another line, and the whole command scrolls rather than being cut; a changed confirmation re-arms the 0.6 s delay, which also holds for a click on Yes; keys that aren't plain words are quoted too. Yes and No send the `id` of the confirmation on screen, so an answer never lands on a newer one.
- **Audio:** the microphone through `AVAudioEngine`, converted to 16 kHz mono s16le and sent in 320 ms chunks. The app asks for microphone access on first use; denied shows a button to the Privacy settings.
- **Test hook:** `CLUBMATE_VOICE_FILE=<raw 16 kHz s16le file>` makes the app stream that file in real time instead of the microphone, then send. `scripts/test-app.sh` passes `CLUBMATE_VOICE_*`, `HF_HOME` and `UV_CACHE_DIR` through, so the test instance reuses the cached model.
- **Code:** `VoiceController` in ClubmateKit (`Voice.swift`) owns the utterance; the app's `VoiceCapture`, `VoiceButton` (with `VoiceHotkeys`) and `VoicePanel` are thin.

## Phone
- **Trigger:** a mic on every screen of a paired Mac. Where the screen has a composer (a session, a new session) it is the first button of the composer row, left of Attach; elsewhere it floats bottom-left and scroll views get a bottom content margin so the last row can scroll clear of it. Tap to start and tap again to send; press and hold to talk while held (the same 250 ms rule as the Mac). While the Mac is unreachable it shows a crossed-out mic, and a tap shows "Mac offline" for 2 s. Hidden on the Macs list with two or more Macs and for a Mac whose `hello` does not list `voice.start`.
- **Target:** the utterance goes to the Mac of the current screen and its selected workspace; `voice.changed` from other Macs is ignored.
- **Pointer:** `{device: "phone", section}`: `agents` with `selection.session` while a session is open, `backlog` with `selection.task` on a task, the plugin section's id on a More screen (`cron`, `processes`, …), otherwise the plugin id (`docs`, `inbox`). The Macs list of a single paired Mac points at `agents` with no selection.
- **Panel:** a card over the screen, above the composer and a docked request card in the composer's column (iPad: at most 480 pt wide), or above the floating mic, the same content as the Mac's panel; a long confirmation scrolls inside it. Yes (filled) and No; with a hardware keyboard ⌥↩ and Esc. Yes is ignored for 0.6 s after a confirmation shows; Yes and No send the `id` of the confirmation on screen.
- **Audio:** `AVAudioSession` `.record` and `AVAudioEngine`, 16 kHz mono s16le in 320 ms chunks. Each `voice.audio` call takes everything captured since the last one (at most 64 KiB), so on a slow link chunks batch instead of falling behind. The audio session is released when listening stops, so music and calls resume. An interruption (a call) or the app going to the background ends the utterance with `voice.end`; going to the background holds a `UIApplication` background task until that call returns, so it reaches the Mac before the app is suspended. A failed `voice.audio` stops capture and shows the error; nothing is retried.
- **Test hook:** Debug builds stream `CLUBMATE_VOICE_FILE` like the Mac (`SIMCTL_CHILD_CLUBMATE_VOICE_FILE` with `simctl launch`). The fixture server (`-fixtures`) answers the voice commands from `Fixtures/voice-states.json`: `-voice YES` starts an utterance once the Mac is online, `-voicePhase <phase>` pins `voice.state` to that phase; without it the utterance goes listening → thinking → confirm.
- **Code:** `VoiceSession` in CompanionKit (`Voice.swift`) owns the utterance; `VoiceCapture`, `VoiceButton` (with `ComposerMic`, `VoiceOverlay` and `.voiceDock()`, which every bottom composer calls) and `VoicePanel` in `client/Companion/App` are thin. `VoiceState`, `VoicePointer` and `VoiceAudio` live in ClubmateProtocol, shared with the Mac, so both format a confirmation the same way.

## Commands
All are hidden. The client commands are `remote: true`, so the phone calls them through the `remote` plugin.

| Command | Args | Result |
|---|---|---|
| `voice.start` | `{pointer: {device: "mac"\|"phone", section, selection: {session?, task?, file?, line?}}, lang?: "ru"\|"auto"}` (default `ru`) | `{utterance: "vu_…"}`; phase `loading` while the model loads (audio is kept, at most 60 s), else `listening`. Fails with `voice needs uv (https://docs.astral.sh/uv/)` when `uv` is missing. |
| `voice.audio` | `{utterance, pcm}`: base64 PCM s16le, 16 kHz mono; 320 ms chunks (10 240 bytes) recommended, at most 64 KiB raw per call | `{partial}`. After 60 s of audio the rest is cut and the utterance ends itself. `unknown utterance` once it ended or was cancelled. |
| `voice.end` | `{utterance}` | `{heard}`; routing goes on in the background. |
| `voice.cancel` | `{utterance}` | `null`; a pending confirmation is declined. |
| `voice.state` | `{utterance}` | `State` |
| `voice.confirm` | `{utterance, approve, confirm?}` | `null`; `false` ends with result `cancelled`. `confirm` is the `id` of the confirmation the user saw; when another one is pending it fails with `the confirmation changed; answer the one shown now` and nothing runs. |
| `voice.dictionary` (app) | – | `{entries: [{from, to}]}`. Until the user saves their own, it is a built-in seed of Russian spellings the recognizer gives IT words (ворктри / ворк три → worktree, мсп / эм си пи → MCP, бэклог → backlog, мерж → merge, сквош → squash, пиар → PR, си ай → CI, гит → git, клод → Claude, клабмейт → Clubmate, компаньон → Companion, нвим → nvim, …; `seedDictionary` in `plugins/voice/dictionary.go`), and `heard` uses it. |
| `voice.dictionary-set` (app) | `{entries}` | the stored dictionary; it replaces the seed, which is never merged back (an empty list means no dictionary). Matching is whole-word, case-insensitive, longest `from` first. |

`State` = `{utterance, phase, partial?, heard?, plan?, result?, error?, confirm?: {id, title, command, args}, session?, rev}`. `phase` is `loading`, `listening`, `thinking`, `confirm`, `done`, `error` or `cancelled`. `session` is the Voice session's id, so a client can open its transcript. Every change publishes `voice.changed {workspace, utterance, rev}`, and the `remote` plugin forwards it to the phone.

## Context
The pointer carries ids only; the plugin resolves them:
- **Session:** title, status, the pending question (`fields` with options) and the pending permission request (options with `kind`), from `agents.sessions` and `agents.transcript`. A plain question in the agent's last message comes through as `{text}`.
- **Task:** the backlog task.
- **View:** the section's `<section>.main` SDUI tree from `ui.get`, or `{"truncated": true}` when over 8 KiB.
- **Workspace:** name, branches, sessions; `needsYou` lists the sessions waiting for the user. Branches are those of the main checkout and the current worktree, plus at most 10 other worktree branches, newest commit first (`committed` in `git.worktrees`). Sessions and `needsYou` hold at most 50 each; a task over 8 KiB keeps only `{id, title, status, truncated: true}`.
- **No selection:** in Agents and Backlog the plugin reads the `selected` value of `<section>.sidebar`. With nothing selected in Agents, it takes the session that has needed the user longest.

## The Voice session
- **What it is:** one per workspace, titled "Voice", created on first use (its id is in plugin storage). It is reused while `agents.sessions` lists it, and recreated once when it cannot be prompted.
- **How it is set up:** the plugin creates it with `mode: bypassPermissions`, notifications muted, its own system prompt (`plugins/voice/prompt.md`), and `disallowedTools` = the built-in tools plus the MCP tools of every dangerous command. It has no shell and no file tools, and the Agents sidebar never selects it on its own: not when it is created, and not as the fallback when the selected session goes away or it is the only one (any session started with `disallowedTools`, `systemPrompt` or `mode` is skipped there). Clicking it still selects it.
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
