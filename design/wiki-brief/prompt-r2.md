# Clubmate Wiki, round 2: a folder that has its own page

Round 2 on this canvas, building on direction B ("Margin", which is built now).

## What the user asked for

The wiki's hierarchy is folders in the page path (`guides/sdui/nodes.md`). Until now a folder was only a fold. The user asked that a folder can have its own page: `<folder>/index.md` (else `README.md`). The folder row opens that page.

## What is built (not landed; screenshots uploaded as assets)

- `/_blob/648a2eb6e3100303866c309f4d129098`: Mac, 900 pt, dark. The folder `guides/sdui` has an index page "SDUI protocol" and is selected; its page is open on the right.
- `/_blob/085c843b736f8905dedf779497f515ab`: the same at 640, light.
- `/_blob/2455fa7f7146cb97e8e78e9cc85756f6`: another seed at 900.

The rules as built:

- The folder row takes the index page's title as its label (the full title in the tooltip). The index page isn't listed again inside the folder.
- Selecting the folder row opens the page and highlights the row, by click or ↩. h/l and the arrows still fold it.
- In a single-project wiki, top-level folders draw as **section labels** (muted, no icon; rule 5: "folder rows without folder icons"). So a top-level folder with a page is now a section label that can be selected. That is new for section labels anywhere in the app; until now they only folded.
- Problems you can see in the shots:
  - A selected section label next to page rows reads oddly.
  - A folder with a page looks the same as a plain folder until you hover or select it.
  - A long index title in a section label is cut ("Protocol overview with a rathe…").

## Questions

1. **How does a folder that has its own page look in the tree,** next to plain folders and pages? Consider a top-level folder (today a section label) and a nested folder (today a fold row). Does it get an icon or a mark? Is its label the page title or the folder name?
2. **Selection and keys:** what does a click on the row do, and what does ↩ do, given the row also folds? Should the chevron be its own target?
3. **The page itself:** does an index page show anything extra, such as the folder's pages as a list in the margin column ("In this folder"), or a breadcrumb in the header (`guides / sdui`)? Should a page inside a folder show where it sits?
4. **Companion (iPhone and iPad).** The Companion renders the same SDUI trees: the iPad has a sidebar tree, and the phone has lists. Neither shows the wiki yet, but the tree change is shared. How should a folder with a page behave there (a tap opens the page or folds it; a disclosure)?
   - Draw one iPad board, in the companion language: `C-*` boards on the shared canvas. The rules are in `design/companion/`.

## Constraints

- Keep the boards' language: colour roles, the monospaced terminal font, compact, keyboard-first.
- Tag each part:
  - (a) token or renderer change;
  - (b) new SDUI node or prop;
  - (c) plugin content;
  - (d) new plugin logic;
  - (e) doc stylesheet.
- Prefer (a), (c) and (e). Section labels and tree rows are shared by every section: say if a change leaks outside the wiki.

## Deliverables

Page `r2`, at most 6 boards:

- 2–3 directions for the tree row, each at 1280 Mate dark and 640 Mate light, with a sidebar holding a top-level folder with a page, a nested one, a plain folder and pages.
- An iPad board for your pick.
- Your recommendation, and a short handoff tagged (a)–(e).
