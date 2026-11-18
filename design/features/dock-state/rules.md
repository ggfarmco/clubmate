# Dock state — decided 18 Nov (direction A)

Round `r-dock` on the canvas "Clubmate · Gates" (https://claude.ai/artifact/AWj3XQrfvLK6aoR8XqcjQr, page r-dock). The user picked A, the designer's recommendation. B (a pip on the glyph) and C (a second line) are in `rounds/`. Brief: `design/gates-dock-brief/`.

## Rule (all three dock rows: Service, Gates, Graphs)

1. The count column holds only the row's number (service sessions, gates, graphs), right-aligned in caption ink. The + keeps its own column after it.
2. What is happening is one state, the row's highest by priority, in words after the label. Never in the count column.
3. Attention (`N to trust`, `asks you`) is an amber badge. Moving (`N asking`, `N landing`, `N held`) is "· ‹state›" in caption ink.

| Row | States, highest first |
|---|---|
| Service | asks you (badge) |
| Gates | N to trust (badge) › N asking › N landing › N held |
| Graphs | N to trust (badge) |

`N held` takes the plain slot after `landing` (the designer's note); a row never shows two states, so "1 landing · 2 held" is gone.

## Separator

"·", the house separator (`RD-A-Mac` draws parentheses as its `sep` tweak; not chosen).

## Phone and iPad (`C-RD-Phone`)

The state goes into the row's subtitle, followed by "· ‹which›" when one gate or graph is behind it ("1 landing · into main", "1 to trust · Plan pipeline"); the count stays on the right. Attention tints the glyph and the state word amber (the glyph keeps its outline shape: Graphs has no filled symbol).

## Handoff

- (a) Count column: always the number. Mac: `TreeDockView` passes the state as an `ItemNote` after the label (`ItemRow.note`).
- (c) Words: "asks you", "N to trust", "N asking", "N landing", "N held".
- (d) `dock[]` rows carry `state`, `which` and `attention`; `detail`/`phoneDetail` are the count. The SDUI `tree.dock` row carries `state`.
