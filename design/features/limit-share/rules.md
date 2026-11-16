# Usage limits on the phone — decided 16 Nov

Direction A, "in place". Boards: `A-final.jpg` (List, Accounts, Session info, warning, reached), `C-UL-A-List`, `C-UL-A-Accounts`, `C-UL-A-Session-Light`, `C-UL-A-Close-Light`, `C-UL-A-Reached`; rules board `UL-Rules.png` / `UL-Rules.dc.html`. Rendered from the Claude Design project "Clubmate design".

User rulings: direction A; paired Macs can be on different Claude accounts; warning stays amber text on the Mac and the phone.

## Decisions

1. **A, in place.** The account's two windows are one caption line in the workspace header; the session's 5-hour share joins its subtitle from 1%; ••• → Session info holds the rest.
2. **Close to a limit** the line changes: amber text on warning, danger text when reached, the same on Mac and phone. Sessions the limit stopped gather under "Paused until ‹reset›" after Needs you, on phone and Mac. No banner, no notification, nothing in Needs you.
3. **Accounts:** paired Macs can use different Claude accounts. The plugin sends an account id with the windows; the phone keeps each account's numbers apart and never adds them up.
4. The Mac keeps its header text and status bar; the iPad carries the line in its sidebar footer.

## Question by question

- **Account limits.** Workspace header, right-aligned on the Mac line: `5h 42% · 7d 81%`, caption ink with the numbers in secondary ink, no reset times while both are allowed. The line is per workspace: the account of that workspace's Mac. Tapping it opens **Claude usage**: this Mac's account first, then any other account your Macs use, each with both windows, resets and "via ‹Mac› · 2 min ago". A window not reported yet is left out; a window whose reset time has passed is hidden until the next report.
- **A session's share.** Session subtitle: "Waiting for you · shop · ≈6% of 5h", the 5-hour share only, from 1%. ••• → **Session info**: context, the share of both windows, one sentence on the estimate, and the **Claude account** group naming the account (its email) above its two windows. List rows show no share; ≈ marks every share as an estimate.
- **Close to a limit.** Warning: the line narrows to the window in trouble and adds its reset, "5h 91% · resets 19:03", amber text. Reached: "5h limit reached · resets 19:03", danger text; stopped sessions move into Paused until 19:03 after Needs you, each reading "Stopped by the usage limit · resumes 19:03". The 7-day window works the same ("7d limit reached · resets Wed").
- **The Mac.** The header's `≈6% of 5h · ≈3% of 7d` and the status bar stay, warning in amber text. The Mac takes the same plugin words and the same "Paused until ‹reset›" group in its tree.
- **iPad.** As the phone, plus the account line all the time in the sidebar footer (so the workspace header doesn't repeat it) and both shares in the session header's meta line, as on the Mac.

## Several accounts

| Case | Workspace header line | Claude usage (tap) · Session info |
|---|---|---|
| All paired Macs on one account | `5h 42% · 7d 81%`, no account label | one account, named by its email; the newest report from any of its Macs wins |
| Macs on two or more accounts | the line leads with a short label: `alex@home · 5h 12% · 7d 30%` | this Mac's account first, then "Your other Macs": each account with its email, the Macs that use it and its own windows |
| Two accounts share the part before "@" | the label adds the domain's first part: `alex@home`, `alex@acme` | full emails |
| A Mac signs in to another account | the line follows the new account id at the next report; the old account's numbers leave with it | the old account stays listed while another Mac still uses it |

## States

The plugin owns the status and the words; Mac and phone draw them.

| Window status (plugin) | Header line | Ink | Elsewhere |
|---|---|---|---|
| allowed | `5h 42% · 7d 81%` | caption; numbers secondary | — |
| allowed_warning | `5h 91% · resets 19:03` (only that window) | amber, text only, never a chip or badge | — |
| rejected | `5h limit reached · resets 19:03` | danger, text only | stopped sessions under "Paused until 19:03" |
| not reported yet | the window is left out; both missing → no line | — | Session info shows "—" |
| resetsAt passed, no new report | the window is hidden | — | — |

## Handoff

- Phone workspace header: the usage text right-aligned on the Mac line, ink by status, tappable to Claude usage.
- Account label: shown only when the phone knows more than one account id; the email up to "@", plus the domain's first part when two accounts share that part.
- Phone session subtitle: append the 5-hour share label when it is 1% or more.
- Phone sheets: Session info behind ••• ("This session", the estimate sentence, "Claude account" with the email row) and Claude usage from the header line (this Mac's account, then your other Macs').
- Render rules: per account id, the newest `updatedAt` wins; leave out a window not yet reported; hide a window once its `resetsAt` has passed; never sum across accounts.
- iPad: the account line in the sidebar footer; both shares in the session header's meta line.
- Words, shared by Mac and phone: "5h 42% · 7d 81%", "5h 91% · resets 19:03", "5h limit reached · resets 19:03", "≈6% of 5h", "<1%", "Share of 5-hour window", "Claude account", "Claude usage", "This Mac's account", "Your other Macs", "via ‹Mac› · 2 min ago", the estimate sentence, "Paused until ‹reset›".
- Grouping: sessions stopped by a usage limit go under "Paused until ‹reset›" after Needs you, in the phone list and the Mac tree; they never count toward Needs you.
- Companion feed: per Mac, the account (`account`, `accountEmail`) and its windows (`used`, `resetsAt`, `status`, `updatedAt`) from `agents.usage`; per session `limitShare`, `shareLabel`, `shareWords`, `shareDetail` in `agents.sessions` ([docs/agents.md](../../../docs/agents.md)).

## Account email

`agents.usage` carries `accountEmail` (Claude Code's `.claude.json` `oauthAccount.emailAddress`) next to `account`. The phone talks to the Mac over Tailscale (WireGuard), not the push relay, so the email stays between the user's devices; it is kept out of `remote.log`.

## Not built yet

The "Paused until ‹reset›" group (Mac and phone) is a later slice.
