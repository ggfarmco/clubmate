# nrev comments in nvim

Claude Design round 1, 26 Nov 2026. Canvas "Clubmate · nrev comments": https://claude.ai/artifact/CVBq2MXgrFRFZ35EMSxLxX. Brief: `design/nrev-comments-brief/`. The user: "I want comments in the code to stand out more."

Boards: `R1-A-Band.dc.html` (`png/R1-A-Band.png`, Clubmate and Clubmate Light), `R1-A-Diff.dc.html` (`png/R1-A-Diff.png`, A in diffview), `R1-Handoff.dc.html` (`png/R1-Handoff.png`, contrast checks and the full group table), all in `boards.html`. Not picked: `rounds/R1-B-Box.dc.html` (a lime box-drawing frame, +2 lines per comment), `rounds/R1-C-Above.dc.html` (above the line, a colour per type, code line tinted).

## Ruling

| Decision | By |
|---|---|
| Direction **A · Band**: each open comment is a tinted band under its line, from the code's indent to the pane edge. No extra lines. | designer's pick; the user: "A is fine" (26 Nov 2026) |
| **One tone for every type**, the issue treatment (`warn` on `vt_warn`): nearly every comment is a `note`, and the neutral `raised` band is 1.01:1 against `DiffChange`, so a note would not stand out. Overrides the three-tone rows below; the type stays as text in the header. | the user: "almost always only note, the issue/question split is not needed"; tone chosen by the builder |
| Not done: blame off in review tabs, `signcolumn = "auto:2"`. | backlog |
| Three tones by what the comment asks: issue amber (band, rule, title); suggestion and question blue (info); note neutral; praise teal (ok). Only an issue gets a coloured band. | designer |
| Why only issues: `vt_info` is the same colour as `DiffChange` (1.0:1) and an `ok` tint would sit on `DiffAdd`, so in diffview a blue band reads as a changed line. | designer |
| Header: icon + type bold. No author, no time. Keys hint only on the comment under the cursor. | designer |
| Sign stays `✎` (not the type icon: `●` `◆` in the sign column mean diagnostics). | designer |
| No `line_hl_group` on the code line: CursorLine, Visual and diff fills keep their meaning. The line number gets the type tone, bold. | designer |
| Blame: right-aligned, short (`You, 28 days ago`), off in review tabs. | designer |
| No new hex: all colours are Mate roles in `nvim-distro/lua/clubmate/mate.lua`. | brief |

## Highlight groups

Define in `nvim-distro/lua/clubmate/mate.lua` (`M.groups`) for both variants. nrev defines each with `default = true` and a `link` fallback, so other colour schemes (Kanagawa, Light Owl) still get a band. `<T>` = Issue, Suggestion, Question, Note, Praise.

| Group | Used for | Clubmate | Clubmate Light |
|---|---|---|---|
| `NrevBand` | bg of every open-comment virt line (not issue), spaces included | bg `raised` #232C26 | bg `raised` #E6DFCF |
| `NrevBandIssue` | the same for an issue | bg `vt_warn` #302E1D | bg `vt_warn` #E9DFCC |
| `NrevRuleIssue`, `NrevTitleIssue` | rule `▌`/`╏`, "● issue"; title bold | fg `warn`, bg `vt_warn` | fg `warn` #8A5810, bg `vt_warn` |
| `NrevRuleSuggestion`, `NrevTitleSuggestion`, `…Question` | rule, "◆ suggestion", "? question"; title bold | fg `info`, bg `raised` | fg `info`, bg `raised` |
| `NrevRuleNote` | rule of a note | fg `muted`, bg `raised` | fg `muted`, bg `raised` |
| `NrevTitleNote` | "· note", bold | fg `fg`, bg `raised` | fg `fg`, bg `raised` |
| `NrevRulePraise`, `NrevTitlePraise` | rule, "+ praise"; title bold | fg `ok`, bg `raised` | fg `ok`, bg `raised` |
| `NrevBody`, `NrevBodyIssue` | comment text (was `Normal`, no bg) | fg `fg`, bg `raised` / `vt_warn` | same |
| `NrevResolution`, `NrevResolutionIssue` | "↳ …" line, italic | fg `muted`, bg `raised` / `vt_warn` | fg `comment`, bg `raised` / `vt_warn` |
| `NrevKeys`, `NrevKeysIssue` | keys hint | fg `muted`, bg as the band | fg `comment`, bg as the band |
| `NrevDrift`, `NrevDriftIssue` | "⚠ code changed" in the header, bold | fg `warn`, bg as the band | same |
| `NrevResolved` | the one-line resolved comment and its `▏` rule, italic, no bg | fg `comment` | fg `comment` |
| `NrevSign<T>` | sign `✎`: issue `warn`, suggestion/question `info`, note `fg`, praise `ok` | fg as listed | fg as listed |
| `NrevSignResolved`, `NrevSignDrift` | sign `✓` / `⚠` | fg `comment` | fg `comment` |
| `NrevNr<T>` | `number_hl_group` on the start line of an open comment, bold | fg as the sign | fg as the sign |
| `GitSignsCurrentLineBlame` | unchanged: no bg, so the band wins | fg `comment`, italic | same |

Secondary text on a band uses `muted` in dark and `comment` in light; the other way round each drops below 4.5:1. Contrast (dark / light): body on `raised` 11.1 / 10.9:1, `warn` on `vt_warn` 7.6 / 4.6, `info` on `raised` 7.3 / 4.6, `ok` on `raised` 7.4 / 4.6. `raised` is 1.01:1 against `DiffChange` in dark: under a changed anchor line the indent notch, the rule and the bold title mark the band's edge.

## Handoff: `lua/nrev/init.lua` (fork `../diffview.nvim`)

1. **Placement.** Keep `virt_lines` below the start line. No blank lines above or below.
2. **Band.** Each open-comment virt line = indent (spaces, no hl) + `▌` in `NrevRule<T>` + one space + text chunks + trailing spaces in `NrevBand<T>` up to the text width (`winwidth − textoff`). Indent = display width of the start line's leading whitespace, max 16 cells. Replaces `RULE = "▎ "`.
3. **Wrap.** Wrap width = text width − indent − 3. Re-render on `WinResized` / `VimResized`. With the buffer in several windows, pad to the widest (virt_lines are per buffer; extra cells are clipped).
4. **Header.** Icon + type in `NrevTitle<T>`. Drift adds two spaces + `⚠ code changed` in `NrevDrift<T>` (was ` ⚠`). No author or time.
5. **Keys hint.** `,rx resolve · ,rd delete` right-aligned with one trailing space, only on the comment whose range holds the cursor. Re-render that comment and the previous one on `CursorMoved`, debounced.
6. **Resolution.** `↳ ` + text in `NrevResolution<T>`; wrapped lines indent 2 more.
7. **Resolved.** One line, no band: indent + `▏` + ` ✓ <type> · resolved · ` + the first words of the body, cut with `…`, all `NrevResolved`. Sign `✓` in `NrevSignResolved`, no number hl.
8. **Drifted.** Band stays; rule `▌` → `╏`; header gets `⚠ code changed`; sign `⚠` in `NrevSignDrift`.
9. **Sign.** Keep `✎` in `NrevSign<T>` (was `sign_hl_group` = `tones`/`hls`). Priority above gitsigns. Clubmate sets `signcolumn = "yes"` (1 cell, `nvim-distro/lua/clubmate/options.lua`); the designer asks for `auto:2` so a git sign and the nrev sign can share a line.
10. **Code line.** `number_hl_group = NrevNr<T>` on the start line of an open comment. No `line_hl_group`.
11. **Diff filler** (`lua/nrev/diffview.lua`). The other side gets the same number of filler virt lines: indent + spaces in the same `NrevBand<T>`, no rule, no text. A resolved comment's filler is one empty line.
12. **Blame** (`nvim-distro/lua/clubmate/plugins/git.lua`, gitsigns setup). `current_line_blame_opts.virt_text_pos = "right_align"`, `current_line_blame_formatter = "<author>, <author_time:%R>"`. In a review tab nrev calls `toggle_current_line_blame(false)` on open and restores the old value on close.
