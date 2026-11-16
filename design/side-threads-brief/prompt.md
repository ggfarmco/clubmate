# Clubmate: side threads

Hi. This is a new question for the Clubmate boards in this project (`design/language`, `design/companion`). The user wants a new feature and isn't sure how it should look. Please give us directions as boards. Draw on top of your own boards; the screens involved today are:

- Mac question card: `Q1-Ask-1280-Mate`, `Q1-Ask-640-MateLight`
- Mac transcript and Agents tree: `Agents-Mate-1280`, `Agents-OneTree-1280-Mate`, `Agents-640-MateLight`
- Phone question card and session: `C-Question`, `C-Question-Light`, `C-Q1-Session`
- Handing a session to another session (closest existing "move work elsewhere"): `Handoff`, `C-Handoff`

## 1. The problem, in the user's words

When an agent asks a structured question (a card with options), the user often wants to dig into one option before picking it. Asking in the main session pollutes its context and transcript. The same happens with a passage of an agent's answer: "what did you mean here?" shouldn't land in the main conversation.

The user's idea is a **side thread**: a lightweight conversation forked off a point in a session.

- **From a choice.** On an option of a pending question: "Ask about this option" opens a side thread that already knows the session's context, the question and that option. The user discusses it there, then comes back and picks.
- **From a selection.** Select a piece of agent output (no question pending) and "Ask aside" opens a side thread about that passage.

Afterwards the user wants three ways out of a side thread:

1. **Keep it as its own session**: promote it to a normal, standalone session.
2. **Send an answer back** to the session it came from.
3. **Just close it.**

## 2. What is decided (mechanics)

- A side thread is a real agent session: a **fork** of the parent's conversation at the moment it opens. It reads everything the parent knew; nothing it says reaches the parent unless the user sends it back.
- It is **read-only** by default: it can read files and think, but it does not edit the worktree (the agent's plan mode). Promoting it lifts that.
- It hangs **under its parent** in the Agents tree, like a child session, and is cheap to throw away. Closing it deletes it.
- The parent keeps running or waiting as before. A pending question stays pending while the side thread is open.
- It works on the Mac and on the phone.

## 3. Questions for you

1. **Where it opens.** On the Mac: beside the parent (a split or drawer, so the question card stays visible), over it (a popover or sheet), or as an ordinary session you navigate to? On the phone: a sheet over the session, or a pushed screen? The user's main case is "look at the options, ask about one, come back and pick", so the trip back matters.
2. **Entry from an option.** How does an option row offer "Ask about this option" without cluttering a card that holds up to 4 questions × 4 options? Hover button, context menu, a key? On the Mac it must be reachable from the keyboard.
3. **Entry from a selection.** On the Mac, agent prose is selectable by range, but today only the stock Copy / Look Up menu appears. On the iPhone, transcript text can only be selected as a whole block (Copy/Share). How does the user point at a passage on each? Is "the whole message" good enough on the phone?
4. **How it is marked.** A side thread in the Agents tree and the phone's session list: how is it told apart from a child an agent started? Where does the parent's transcript show that a side thread was forked from this option or passage (an anchor), and can the user jump between them?
5. **Send back.** What gets sent: a summary the side thread's agent writes on request, a quote the user picks from it, or its last message? Where does it land: the parent's composer as a draft the user can edit, or, when the thread came from a question, the answer to that question (e.g. the chosen option plus a note)? Show the preview or edit step, if any.
6. **Promote and close.** Where do the three ways out live (thread header, toolbar, end of the thread)? What changes visibly when a read-only side thread becomes a normal session that can edit? Does closing ask when the thread has something unsent?
7. **Several threads.** A user may open one per option, or several on one parent. How do they stack in the tree, on the parent and on the phone?
8. **Needs you.** Does an open side thread count toward the parent's or the user's "Needs you"? Our lean: no, unless the side thread itself is waiting on a question; propose the states and labels. The rule will live in the Go plugin, not the clients.

## 4. Constraints

- Keep the boards' design language (`Principles`, `Patterns`, `Components`; companion `C-*`). The theme comes from the user's Ghostty palette: colour roles, not hex values. Font is the terminal font at `uiSize`.
- On the Mac the question card and the transcript are SDUI trees the Go plugin builds from existing nodes (card, text, choice, button, stack); the prose text view and the composer are native. On the phone the question card and transcript are drawn natively. For each part of your design, say whether it is (a) a token/renderer change, (b) a new SDUI node or prop, (c) plugin content (words, grouping, order), or (d) new plugin logic (a new state, command or rule). Prefer (a) and (c); say plainly what needs (d).
- Mac and phone show the same meaning. The plugin decides what a state means; clients only render it.
- Keyboard first on the Mac: say which keys open, send back, promote and close a side thread.
- The question card's data shape is fixed by Claude Code's AskUserQuestion: questions, each with a header chip (≤12 chars), options (label + description), a multi-select flag and a free-text Other.

## 5. Deliverables

- 2–3 distinct directions for questions 1–6. For each: the Mac at 1280 Mate dark and 640 Mate light (the parent with a pending question and an open side thread; a side thread opened from a selection; the send-back step), the Agents sidebar with a side thread, and the phone (dark and light) for the same moments.
- Your recommendation, plus answers to 7–8 in the chosen direction.
- A handoff list: every change tagged (a)/(b)/(c)/(d).
