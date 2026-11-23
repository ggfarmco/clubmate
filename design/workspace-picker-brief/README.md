# Workspace picker brief

Input for a Claude Design round on the companion's workspace picker (23 Nov 2026). The user: "Хочется как-то с этим списком красивее поступить. Может дизайнер предложит пару вариантов."

- `prompt.md`: the brief sent to the Design chat (one paragraph).
- `screens/`: today's screens. `iphone-menu-real-light.jpg` is the user's phone (2 Macs, Home first). The others come from the simulator with fixtures (`design/companion-brief/README.md`, `-macs 3`): `iphone-agents-{dark,light}.png` (closed capsule on the Agents root), `iphone-menu-3macs-light.png` and `ipad-menu-3macs-light.png` (the open menu, from the Home workspace round, 22 Nov).

## Today in code

`client/Companion/App/WorkspaceCapsule.swift`: a system `Menu`, one `Section` per Mac ("<name> (N) · <presence>"), Home first (`Workspace.homeFirst`), a folder glyph per workspace (checkmark on the current one), "n waiting" from `CompanionModel.waitingCount` (summed on the phone from `agents.sessions`), Mac Details, Pair a Mac. Rules: `design/features/device-ux/rules.md` § Workspace capsule.

## Round 1

Canvas "Clubmate · workspace picker": https://claude.ai/artifact/K2F99UwNrVjsXFNUBSucj2, chat https://claude.ai/chat/fc7bdd23-a9ee-4d7d-ad84-8d517d9335df. Pages: Today (`Main`: every screenshot named), Language (`C-Language`, `C-Components`, `C-Workspace`, `C-Workspace-Light`, `C-Macs`, `C-Offline`, `C-MacInfo`, `C-iPad`), r1. Canvas copies of cm.css / c.css: `/_blob/a3008f02…` / `/_blob/a360d4e2…`.

- `round-1.json`: the source of `round-1.html` (boards in `boards/r1/`; `r1-*-Light.dc.html` are light copies for the page, the canvas draws them with `dc-import`). `boards/r1/` is the round as the canvas had it on 23 Nov, kept only to rebuild `round-1.html`; the source of truth for the picked design is `design/features/workspace-picker/`, and its copies of the same boards win where they differ.
- Directions: A · Refined system Menu (S), B · Mac cards sheet (L), C · Mac strip sheet (M, the designer's pick).
- Ruling (user, 23 Nov): **A**. Boards, PNG and `rules.md` are in `design/features/workspace-picker/`; B, C and the iPad board in its `rounds/`.
