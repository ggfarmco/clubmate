# Mac: how an agent question looks today

Captured 2026-11-04 from `96862c78` (Debug build, `make app`), one test instance (`scripts/test-app.sh start`), `ui snapshot` at 2× scale. Nothing in the product changed.

## Setup

- Agents: `plugins/agents/internal/fakeagent` with `fakeagent-script.json` (here), `CLUBMATE_AGENTS_COMMAND` set to it. Four sessions from `cm agent spawn`: Bump (done), Port cron (running), Rework the Send sheet (done, the long report), Rate-limit (waiting on an `elicitation/create` form shaped like Claude's AskUserQuestion: `question_N` + `question_N_custom`).
- Because `cm agent spawn` started them, every prompt is labelled "from another agent".
- Themes as in `design/check/mac/notes.md`: `*-1280` Kanagawa Wave + `-AppleInterfaceStyle Dark`; `*-800` Catppuccin Latte + `-NSRequiresAquaSystemAppearance YES`, `--size 640x800`, clamped to the 800 pt minimum (1600×1600 px).
- Selection and filter via `cm ui event --slot agents.sidebar` (`--node sessions --action select`, `--node filter --action filter --value '"needs"'`).
- The "…" in the given report text was filled with a plausible five-bullet report; the rest is verbatim.

## Files

- `question-pending-1280/-800`: Rate-limit session, question card with a single choice (Scope) and a multi-select (Exempt), 3 options each. Today the single choice renders as a collapsed "—" popup, so its options and their descriptions are not visible; the multi-select shows switches without the descriptions; each question gets an extra "Other" field.
- `report-buried-decision-1280/-800`: Rework the Send review sheet, done; the decision (#185) sits in the last paragraph of the report, the session reads as done ✓ and is not under Needs you.
- `needs-you-sidebar-1280/-800`: sidebar with the Needs you filter, one session "asking", the same question card on the right.
