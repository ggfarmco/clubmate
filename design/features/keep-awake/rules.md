# Keep Awake: the design

The final boards from Claude Design (canvas "Clubmate · Keep Awake", round 2, 16 Nov 2026): `KA2-*.dc.html` are the sources, `1-…5-*.png` captures of them. The plugin's contract is `docs/awake.md`; build from these files, not the canvas.

## Rulings by the user

- **One switch, three choices**, at most one active: For N hours · While agents work · Until turned off; Turn Off while one is active. No separate agents section, no timed agents rule, no "Until 08:00".
- **Hours: 1 / 2 / 4 / 8 / 24.** The boards draw 1 / 2 / 4 / 8; 24 is added everywhere they list the hours (Mac submenu, Settings select, phone palette row). A time that isn't today reads with its weekday ("Until Tue 18:00").
- Settings uses the **radio group** of board 3 (a new control in Components 4a), not the one-select fallback.

## What the boards fix

1. **Status bar** (`1-status-bar`): the item lives in the right group, left of the hints and ⌘K, and grows leftwards. Off: faint outline cup. Armed (While agents work chosen, nothing holding): outline cup at muted. Awake: filled cup with steam + `text` at the bar's muted level. No tint. Below 800 pt the cup alone. Click/right-click: menu; ⌥-click: toggle (nothing chosen → Until turned off; else Turn Off). Tooltip `detail`.
2. **Menu** (`2-menu`): first line `text` (or "The Mac sleeps as usual."), one quiet line per hold (title left, trailing right; an agent's line opens its session). Section "Keep Awake": For Hours ▸ (submenu, ✓ on the number; the item reads "For 4 Hours" while active), While Agents Work, Until Turned Off, ✓ on the active choice; Turn Off only while a choice is active. ⌥⌘A sits on Until Turned Off when nothing is chosen and on Turn Off otherwise. Then Keep Awake Settings…
3. **Settings** (`3-settings`): Now (read-only: status line, `detail`, holds as a property list); Keep awake (radio group: Off · For [hours select] + "until 18:00" muted while active · While agents work with its caption and "On battery too" indented under it, laptops only · Until turned off with ⌥⌘A); Display: Keep the screen on; lid footnote, laptops only. Flat rows under section labels.
4. **Phone** (`4-phone`): Mac details, first group: status row (28 pt glyph; accent while awake, neutral off/armed), agents' own holds nested under it (open their session), one "Keep awake" picker (value Off / Until 18:00 / While agents work / Until turned off); its menu: palette row of hours, While Agents Work with subtitle, Until Turned Off, divider, Turn Off while active. Footer `detail` only while awake. Offline: group stays dimmed, "Asleep or offline · Last seen 18:20 · it was awake until 18:00". Macs list: small cup + end time in the state line while awake.
5. **Words and data** (`5-words-data`): every string from the plugin (`text`, `detail`, `value`, `holds`, `labels`); clients send `set {mode, hours}` or `off`.

## Round 3: the gourd (16 Nov 2026)

`KA3-Glyph.dc.html` and `KA3-InPlace.dc.html`: a monochrome mate gourd replaces the cup everywhere it showed, same places and levels (off faint, armed muted, awake foreground; the phone's 28 pt circle keeps the accent while awake). 16-unit grid, stroke 1.4 (1.5 on the phone), round caps and joins. Off and armed: body `M5.7 5.6C4.2 6.7 3.2 8.4 3.2 10.4 3.2 13 5.3 15 8 15s4.8-2 4.8-4.6c0-2-1-3.7-2.5-4.8` stroked, rim `M5 5.6h6`, bombilla `M9.3 5.6 11.6 1.3`. Awake: body `M3.7 7.2C3 8.1 2.5 9.2 2.5 10.4 2.5 13.4 4.9 15.7 8 15.7s5.5-2.3 5.5-5.3c0-1.2-.5-2.3-1.2-3.2Z` filled only (the gap under the rim makes it a vessel), the same rim and bombilla, leaf `M5.5 4.8C4.7 5.1 3.6 4.6 3.1 3.5 4.2 3.1 5.2 3.7 5.5 4.8Z` filled and stroked. Sizes: 14 pt in the status bar, menu and Settings, 16 in the phone's 28 pt glyph, 13 in the Macs list. SDUI names `clubmate.gourd` / `clubmate.gourd.fill`.
