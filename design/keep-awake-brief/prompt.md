# Clubmate: Keep Awake

Hi. This canvas is for one feature that's already built but was never designed: it was done straight from the Clubmate design language, without you. Page **Today** has real screenshots of what's there now (test app + iPhone simulator); page **Language** has the language boards (Principles, Tokens, Components, Patterns, C-Language, C-Components). Please draw your direction on page **Round 1**: at most 6 boards, using the language boards' tokens and components (cm.css / c.css are linked from them).

## What it does

A Clubmate plugin keeps the Mac from sleeping, like Amphetamine. Three things can hold the Mac awake:

1. **You**: for 1 hour, 4 hours, or until turned off.
2. **An agent**: an agent session asks for it during a long build ("release build"), up to 12 h, and it ends when the build or the session ends.
3. **"While agents work"**: a rule, off by default. While it's on, any agent session that is running or waiting for you keeps the Mac awake (on power only, unless "On battery too").

Settings: the rule, On battery too, Keep the screen on. A closed lid still sleeps a laptop on battery; we say so.

The plugin decides every word; clients show two strings as they are:
- `text`, short, only while awake: "Awake", "Awake until 18:00", "Awake · 2 agents", "Awake · release build", "Awake +1" (+N = other holds).
- `detail`, one sentence: "Kept awake by you until 18:00, on battery too. A closed lid still sleeps the Mac on battery." / "The Mac sleeps as usual."

## Where it shows today

**Mac status bar** (bottom bar: `● core`, agent counts, usage, hints, ⌘K):
- Off: a faint outline cup icon only (so the menu stays reachable). (Today, first strip.)
- On: filled cup + `text` in the bar's muted grey. (Today: "Awake until …", "Awake · release build", "Awake +1".)
- Click opens a native menu (no screenshot, it's an NSMenu):
  ```
  Keep Awake for 1 Hour
  Keep Awake for 4 Hours
  Keep Awake Until Turned Off
  Turn Off                      (only while your own hold is on)
  ─────────
  ✓ While Agents Work           (checkmark = the rule is on)
  ─────────
  Keep Awake Settings…
  ```
- Tooltip: `detail`.

**Mac Settings → Keep Awake** (rendered by the plugin with our standard setting rows). (Today.) Keep the Mac awake (toggle, `detail` under it), While agents work, On battery too, Keep the screen on, and a footnote about the closed lid.

**Phone, Mac details** (tap the Mac's name): a Keep Awake group at the top while the Mac is online. (Today, four phone shots.) `text` (or `detail` while off), a "Keep Awake…" menu with the same three durations, Turn Off while your hold is on. The phone has no toggle for "While agents work" yet.

## What the user asked for

"Like Amphetamine": quick on-for-a-while from the status bar, and the agents rule something you switch on there too. They'd also like a timed version of the agents rule ("for 4 hours", "until morning") — open question whether that belongs in the menu.

## What we'd like from you

1. The status bar item: off and on looks, how much text, where it sits among core / agents / usage.
2. The menu: order, grouping, wording, whether the agents rule gets durations too, and whether to show what currently holds the Mac (e.g. "Held by: release build (agent) until 20:40") in the menu.
3. Settings → Keep Awake: is the standard row page right, the footnote?
4. Phone: the Keep Awake group in Mac details, including a While Agents Work toggle; or should it live elsewhere on the phone?

Light and dark, Mac 1280 and 640 where it matters, iPhone — all within the 6 boards on Round 1. Please don't touch the Today and Language pages.
