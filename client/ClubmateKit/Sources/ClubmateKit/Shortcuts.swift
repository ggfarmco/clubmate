import Foundation

public struct Shortcut: Identifiable, Equatable, Sendable {
    public let id: String
    public let keys: [String]
    public let action: String
    public let when: String

    public init(_ id: String, _ keys: [String], _ action: String, when: String = "") {
        self.id = id
        self.keys = keys
        self.action = action
        self.when = when
    }

    public var key: String { keys[0] }

    public var labels: [String] { keys.map(Self.label) }

    public static func label(_ key: String) -> String {
        KeyCombo(key)?.label ?? key
    }

    func matches(_ terms: [String], group: String) -> Bool {
        let text = ([group, action, when] + keys + labels).joined(separator: "\n").lowercased()
        return terms.allSatisfy(text.contains)
    }
}

public struct ShortcutGroup: Identifiable, Equatable, Sendable {
    public let id: String
    public let title: String
    public let shortcuts: [Shortcut]

    public init(id: String, title: String, shortcuts: [Shortcut]) {
        self.id = id
        self.title = title
        self.shortcuts = shortcuts
    }

    public func filtered(_ query: String) -> ShortcutGroup? {
        let terms = query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
        let kept = terms.isEmpty ? shortcuts : shortcuts.filter { $0.matches(terms, group: title) }
        return kept.isEmpty ? nil : ShortcutGroup(id: id, title: title, shortcuts: kept)
    }
}

public enum AppShortcuts {
    public static let new = Shortcut("app.new", ["cmd+n"], "New terminal, or a new item in the current section")
    public static let newHomeTerminal = Shortcut("app.new-home-terminal", ["cmd+shift+n"], "New Home Terminal")
    public static let openHome = Shortcut("app.open-home", ["cmd+shift+h"], "Open Home")
    public static let openFolder = Shortcut("app.open-folder", ["cmd+o"], "Open Folder…")
    public static let openDoc = Shortcut("app.open-doc", ["cmd+shift+d"], "Open Doc…")
    public static let switchPopupFocus = Shortcut("app.switch-popup-focus", ["cmd+j"], "Switch Popup Focus", when: "A popup is open")
    public static let closeSession = Shortcut("app.close-session", ["cmd+opt+delete"], "Close Session")
    public static let close = Shortcut("app.close", ["cmd+w"], "Close terminal session, popup or window")
    public static let palette = Shortcut("app.palette", ["cmd+k"], "Command Palette")
    public static let toggleSidebar = Shortcut("app.toggle-sidebar", ["ctrl+cmd+s"], "Toggle Sidebar")
    public static let toggleChat = Shortcut("app.toggle-chat", ["cmd+b"], "Toggle Chat", when: "A section with a chat panel")
    public static let fontIncrease = Shortcut("app.font-increase", ["cmd+=", "⌘+"], "Increase font size where focus is")
    public static let fontDecrease = Shortcut("app.font-decrease", ["cmd+-"], "Decrease font size where focus is")
    public static let fontReset = Shortcut("app.font-reset", ["cmd+0"], "Reset font size where focus is")
    public static let section = Shortcut("app.section", ["⌘1…⌘9"], "Switch to the Nth section in the rail")
    public static let nextWorkspace = Shortcut("app.next-workspace", ["ctrl+cmd+right"], "Next workspace")
    public static let previousWorkspace = Shortcut("app.previous-workspace", ["ctrl+cmd+left"], "Previous workspace")
    public static let nextSession = Shortcut("app.next-session", ["ctrl+cmd+down"], "Next agent session", when: "Agents")
    public static let previousSession = Shortcut("app.previous-session", ["ctrl+cmd+up"], "Previous agent session", when: "Agents")
    public static let jumpSession = Shortcut("app.jump-session", ["⌃⌘1…⌃⌘9"], "Nth top-level agent session or graph run", when: "Agents")
    public static let lastSession = Shortcut("app.last-session", ["ctrl+tab"], "Last agent session, and back", when: "Agents pane focused")
    public static let nextNeeds = Shortcut("app.next-needs", ["ctrl+cmd+j"], "Next agent session that needs you, oldest first", when: "Agents")
    public static let previousNeeds = Shortcut("app.previous-needs", ["ctrl+cmd+shift+j"], "Previous agent session that needs you", when: "Agents")
    public static let workspaceSwitcher = Shortcut("app.workspace-switcher", ["ctrl+cmd+w"], "Workspace switcher")
    public static let notifications = Shortcut("app.notifications", ["cmd+shift+i"], "Notifications")
    public static let keepAwake = Shortcut("app.keep-awake", ["cmd+opt+a"], "Keep Awake: Until Turned Off, or Turn Off while a choice is active")

    public static let retry = Shortcut("app.retry", ["cmd+r"], "Retry starting the core", when: "Core didn't start or stopped")
    public static let showLog = Shortcut("app.show-log", ["cmd+l"], "Show the core log", when: "Core slow to start or didn't start")

    public static func section(_ index: Int) -> String {
        "cmd+\(index)"
    }

    public static func jumpSession(_ index: Int) -> String {
        "ctrl+cmd+\(index)"
    }

    public static let groups: [ShortcutGroup] = [
        ShortcutGroup(id: "app", title: "App and windows", shortcuts: [
            new, newHomeTerminal, openFolder, openHome, openDoc, close, closeSession, palette, toggleChat, toggleSidebar,
            fontIncrease, fontDecrease, fontReset, section, nextWorkspace, previousWorkspace, nextSession, previousSession, jumpSession, lastSession, nextNeeds, previousNeeds, workspaceSwitcher, notifications, keepAwake, retry, showLog,
            Shortcut("voice.talk", ["opt+space"], "Voice: hold to talk, release to send", when: "While voice is on"),
            Shortcut("voice.confirm", ["return", "esc"], "Voice confirmation: yes, no", when: "Voice panel"),
            Shortcut("voice.cancel", ["esc"], "Cancel the utterance before it is sent", when: "Voice panel, Sending"),
            switchPopupFocus,
            Shortcut("popup.close", ["esc", "cmd+w"], "Close the popup over the workspace", when: "Popup focused"),
            Shortcut("popup.zoom", ["cmd+=", "cmd+-", "cmd+0"], "Zoom the doc in, out, reset", when: "Popup shows a doc"),
            Shortcut("popup.pop-out", ["cmd+shift+o"], "Pop the popup out into its own window", when: "Popup focused"),
            Shortcut("popup.find", ["cmd+f"], "Find in the doc", when: "Popup shows a doc"),
            Shortcut("popup.find-step", ["return", "shift+return", "cmd+g", "cmd+shift+g"], "Next, previous match", when: "Find bar open"),
            Shortcut("popup.find-close", ["esc"], "Close the find bar", when: "Find bar open"),
            Shortcut("doc.diagram", ["+", "-", "0", "esc"], "Enlarged diagram: zoom in, out, fit, close", when: "Markdown doc"),
        ]),
        ShortcutGroup(id: "lists", title: "Lists, palette and pickers", shortcuts: [
            Shortcut("lists.move", ["up", "down"], "Move the selection"),
            Shortcut("lists.move-ctrl", ["ctrl+j", "ctrl+n", "ctrl+k", "ctrl+p"], "Move the selection down, up", when: "Text field"),
            Shortcut("lists.grid", ["h", "j", "k", "l"], "Move left, down, up, right", when: "Switcher grid"),
            Shortcut("lists.run", ["return", "ctrl+l"], "Run, open or choose"),
            Shortcut("lists.close", ["esc"], "Close"),
        ]),
        ShortcutGroup(id: "trees", title: "Trees", shortcuts: [
            Shortcut("trees.move", ["j", "down", "k", "up"], "Move the cursor", when: "Sidebar focused"),
            Shortcut("trees.expand", ["l", "right"], "Expand the item, or step into it", when: "Sidebar focused"),
            Shortcut("trees.collapse", ["h", "left"], "Collapse the item, or go to its parent", when: "Sidebar focused"),
            Shortcut("trees.select", ["return"], "Select a leaf, or expand or collapse an item", when: "Sidebar focused"),
            Shortcut("trees.open", ["o"], "Select, also an item with children", when: "Sidebar focused"),
            Shortcut("trees.field", ["gc"], "Focus the sidebar's text field", when: "Sidebar focused"),
            Shortcut("trees.jump", ["g1…g9"], "Nth top-level agent session or graph run", when: "Agents sidebar focused"),
            Shortcut("trees.last", ["gl"], "Last agent session, and back", when: "Agents sidebar focused"),
            Shortcut("trees.needs", ["gn", "gN"], "Next, previous agent session that needs you", when: "Agents sidebar focused"),
            Shortcut("trees.panes", ["gt", "gi"], "Stay in the tree, go to the prompt", when: "Agents sidebar focused"),
            Shortcut("trees.chips", ["<", ">"], "Previous, next filter chip", when: "Sidebar focused"),
            Shortcut("trees.keys", ["?"], "Show the keys of the focused pane", when: "Agents sidebar focused"),
            Shortcut("trees.back", ["esc", "ctrl+l", "ctrl+h"], "Back to the main pane", when: "Sidebar focused"),
        ]),
        ShortcutGroup(id: "sidebar-lists", title: "Sidebar lists", shortcuts: [
            Shortcut("sidebar-lists.move", ["j", "down", "k", "up"], "Move the selection", when: "Terminals, Processes, Cron, Graphs"),
            Shortcut("sidebar-lists.open", ["return"], "Open the selected item in the main pane", when: "List focused"),
            Shortcut("sidebar-lists.back", ["esc", "ctrl+l", "ctrl+h"], "Back to the main pane", when: "List focused"),
        ]),
        ShortcutGroup(id: "tables", title: "Tables", shortcuts: [
            Shortcut("tables.move", ["j", "down", "k", "up"], "Move the selection", when: "Table focused"),
            Shortcut("tables.expand", ["l", "right"], "Expand the row, or step to its first child", when: "Table focused"),
            Shortcut("tables.collapse", ["h", "left"], "Collapse the row, or go to its parent", when: "Table focused"),
        ]),
        ShortcutGroup(id: "composer", title: "Agent composer", shortcuts: [
            Shortcut("composer.send", ["cmd+return"], "Send the prompt, or steer the running turn", when: "Prompt focused"),
            Shortcut("composer.step-out", ["esc", "ctrl+["], "Step out to the transcript; the turn keeps running", when: "Agents, prompt focused"),
            Shortcut("composer.stop", ["esc"], "Stop the running turn", when: "Code or Wiki chat panel, prompt focused"),
            Shortcut("composer.attach", ["@"], "Attach a file", when: "At the start of a word"),
            Shortcut("composer.commands", ["/"], "List the agent's commands", when: "At the start of the prompt"),
            Shortcut("composer.list", ["up", "down", "ctrl+k", "ctrl+j"], "Move in the command list", when: "Command list"),
            Shortcut("composer.insert", ["return", "tab"], "Insert the highlighted command", when: "Command list"),
        ]),
        ShortcutGroup(id: "agents-panes", title: "Agents panes", shortcuts: [
            Shortcut("agents-panes.step-out", ["esc", "ctrl+["], "Step out: prompt, transcript, tree, back to the prompt; never ends full screen", when: "Agents"),
            Shortcut("agents-panes.stop", ["cmd+."], "Stop the running turn; on a running graph run's page, stop the run", when: "Agents"),
            Shortcut("agents-panes.tree", ["opt+cmd+left"], "Go to the sessions tree", when: "Agents"),
            Shortcut("agents-panes.main", ["opt+cmd+right"], "Go to the main pane's prompt; from the main pane, to the aside", when: "Agents"),
            Shortcut("agents-panes.transcript", ["opt+cmd+up", "ctrl+k"], "Go to the transcript of the column you are in", when: "Agents"),
            Shortcut("agents-panes.composer", ["opt+cmd+down", "ctrl+j"], "Go to the prompt of the column you are in", when: "Agents"),
            Shortcut("agents-panes.column", ["ctrl+h", "ctrl+l"], "Column left, right: tree, main pane, aside", when: "Agents"),
            Shortcut("agents-panes.keys", ["cmd+/"], "Show or hide the keys of the focused pane", when: "Agents"),
        ]),
        ShortcutGroup(id: "transcript", title: "Agent transcript", shortcuts: [
            Shortcut("transcript.move", ["j", "down", "k", "up"], "Next, previous block: prompt, thought, plan, actions, answer, card", when: "Agents, transcript focused"),
            Shortcut("transcript.fold", ["l", "right", "h", "left"], "Unfold, fold the block; into, out of an actions group", when: "Agents, transcript focused"),
            Shortcut("transcript.toggle", ["return"], "Fold or unfold the block; on a pending card, give the card the keys", when: "Agents, transcript focused"),
            Shortcut("transcript.ends", ["gg", "G"], "First block (Show earlier first), newest block and follow new output", when: "Agents, transcript focused"),
            Shortcut("transcript.copy", ["cmd+c"], "Copy the focused block as Markdown; a text selection copies as usual", when: "Agents, transcript focused"),
            Shortcut("transcript.file", ["gf"], "Open the focused block's file in nvim", when: "Agents, a Read or Edit block focused"),
            Shortcut("transcript.sessions", ["g1…g9", "gl", "gn", "gN"], "Nth session, last session, next or previous that needs you", when: "Agents, transcript focused"),
            Shortcut("transcript.panes", ["gt", "gi", "gc"], "Go to the tree, the prompt, the search field", when: "Agents, transcript focused"),
            Shortcut("transcript.keys", ["?"], "Show the keys of the focused pane", when: "Agents, transcript focused"),
            Shortcut("transcript.type", ["i", "a"], "Go to the prompt, the draft kept", when: "Agents, transcript focused"),
            Shortcut("transcript.page", ["space", "shift+space"], "Page down, up", when: "Agents, transcript focused"),
            Shortcut("transcript.full-diff", ["cmd+d"], "Full diff of the focused edit, else of the newest edit, in nvim"),
            Shortcut("transcript.aside", ["cmd+\\"], "Ask aside about the selection, else the focused answer, else the last answer", when: "Agents"),
        ]),
        ShortcutGroup(id: "tasks", title: "Multi-repo tasks", shortcuts: [
            Shortcut("tasks.sibling", ["opt+up", "opt+down"], "Previous, next repo of the task", when: "A repo child of a task, not in a text field"),
        ]),
        ShortcutGroup(id: "browser", title: "Agent Browser window", shortcuts: [
            Shortcut("browser.done", ["return"], "Done: hand the page back to the agent", when: "During a handoff"),
        ]),
        ShortcutGroup(id: "permissions", title: "Permission requests", shortcuts: [
            Shortcut("permissions.answer", ["opt+1", "opt+2", "opt+3"], "Allow once, Always allow…, Reject"),
            Shortcut("permissions.confirm", ["opt+2"], "Store the rule and allow", when: "Always allow confirm"),
            Shortcut("permissions.cancel", ["esc", "return"], "Cancel", when: "Always allow confirm"),
            Shortcut("permissions.bare", ["1", "2", "3"], "Allow once, Always allow…, Reject", when: "Prompt empty, transcript or card focused"),
            Shortcut("permissions.yes-no", ["y", "n"], "Allow once, Reject once", when: "Prompt empty, transcript or card focused"),
        ]),
        ShortcutGroup(id: "questions", title: "Agent questions", shortcuts: [
            Shortcut("questions.send", ["return"], "Send the form", when: "Text field of the question"),
        ]),
        ShortcutGroup(id: "vscode", title: "VS Code", shortcuts: [
            Shortcut("vscode.keys", ["cmd+w", "cmd+n", "cmd+o", "cmd+p", "cmd+shift+p"], "Go to VS Code: close, new, open, quick open, palette", when: "VS Code focused"),
            Shortcut("vscode.palette", ["cmd+k"], "Stays the Command Palette", when: "VS Code focused"),
            Shortcut("vscode.chat", ["cmd+b"], "Stays Toggle Chat, which does nothing in VS Code", when: "VS Code focused"),
            Shortcut("vscode.popup", ["cmd+j"], "Stays Switch Popup Focus", when: "VS Code focused"),
        ]),
    ]
}

public enum PaneKeysHelp {
    public static func groups(_ pane: FocusedPane?) -> [ShortcutGroup] {
        let ids: [String] = switch pane {
        case .tree, .field: ["trees", "agents-panes"]
        case .transcript: ["transcript", "permissions", "agents-panes"]
        case .composer: ["composer", "permissions", "agents-panes"]
        default: ["agents-panes"]
        }
        return ids.compactMap { id in AppShortcuts.groups.first { $0.id == id } }
    }
}
