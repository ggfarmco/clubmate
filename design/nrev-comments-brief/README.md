# nrev comments brief

Input for a Claude Design round on how nrev review comments look in Clubmate's nvim (26 Nov 2026). The user: "хочется как-то более явно выделять комменты в коде, может с дизайнером обсуди".

- `prompt.md`: the brief sent to the Design chat (one paragraph).
- `today/light-note-in-go.jpg`: the user's screenshot (Clubmate Light): a `note` under a Go line, grey rule and title, body in the code colour, no background, the gitsigns blame on the next line.

## Today in code

Fork `../diffview.nvim`, `lua/nrev/init.lua`: `M.comment_lines` builds the virt lines, `render` puts one extmark per comment on its start line (sign `✎`/`✓`, `virt_lines` below). `lua/nrev/diffview.lua` draws the filler lines on the other side of a diff. Theme roles: `nvim-distro/lua/clubmate/mate.lua`.

## Round 1

Canvas "Clubmate · nrev comments": https://claude.ai/artifact/CVBq2MXgrFRFZ35EMSxLxX, chat https://claude.ai/chat/ee465ccc-2601-4b9c-b15a-b13cdd5a0f36. Pages: Today (`Main`), Language (`T-Nvim-Clubmate`, `T-Themes`, `Review-Rules`), r1. Canvas copy of cm.css: `/_blob/418fb3dc…`; screenshot `/_blob/87659333…`.

- `round-1.json`: the source of `boards/round1.html` (boards in `boards/r1/`, as the canvas had them on 26 Nov; `boards/r1/canvas.json` is a snapshot of the canvas layout).
- Directions: A · Band (designer's pick), B · Box, one accent, C · Above, colour per type; plus A in diffview and the handoff board.
- Designer's pick: **A**. Boards, PNG and `rules.md` are in `design/features/nrev-comments/`; B and C in its `rounds/`. The user has not ruled yet.
