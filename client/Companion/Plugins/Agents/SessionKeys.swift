import SwiftUI

import CompanionKit

struct AlwaysAsk: Equatable {
    let option: String
    let nonce = UUID()
}

struct KeyedAction {
    let label: String
    let run: () -> Void
}

struct PaneKeys {
    let facts: PaneFacts
    let escape: () -> Void
    let go: (PaneArrow) -> Void
}

@MainActor
@Observable
final class PaneFocus {
    var listFocused = false
}

struct SessionKeyActions: Equatable {
    let scope: String
    let enabled: Bool
    let answers: [Int: KeyedAction]
    let send: KeyedAction?
    var stop: KeyedAction?
    var panes: PaneKeys?
    var help: KeyedAction?

    static func == (lhs: SessionKeyActions, rhs: SessionKeyActions) -> Bool {
        lhs.scope == rhs.scope && lhs.enabled == rhs.enabled && lhs.send?.label == rhs.send?.label && lhs.answers.mapValues(\.label) == rhs.answers.mapValues(\.label)
            && lhs.stop?.label == rhs.stop?.label && lhs.panes?.facts == rhs.panes?.facts && (lhs.help == nil) == (rhs.help == nil)
    }

    func pane(_ arrow: PaneArrow) -> AgentsPane? {
        panes.flatMap { PaneLadder.arrow(arrow, $0.facts) }
    }
}

@MainActor
@Observable
final class SessionKeyBoard {
    private struct Entry {
        let id: UUID
        var keys: SessionKeyActions
        var page: StackEntry?
    }

    private var entries: [CompanionTab: [Entry]] = [:]

    func keys(for tab: CompanionTab, top: StackEntry?) -> SessionKeyActions? {
        entries[tab]?.last { $0.page == nil || $0.page == top }?.keys
    }

    func set(_ keys: SessionKeyActions, id: UUID, tab: CompanionTab, page: StackEntry?) {
        if let index = entries[tab]?.firstIndex(where: { $0.id == id }) {
            if entries[tab]?[index].keys != keys { entries[tab]?[index].keys = keys }
            if entries[tab]?[index].page != page { entries[tab]?[index].page = page }
            return
        }
        for other in entries.keys where other != tab { entries[other]?.removeAll { $0.id == id } }
        entries[tab, default: []].append(Entry(id: id, keys: keys, page: page))
    }

    func remove(_ id: UUID) {
        for tab in entries.keys where entries[tab]?.contains(where: { $0.id == id }) == true {
            entries[tab]?.removeAll { $0.id == id }
        }
    }
}

final class SessionKeyToken {
    let id = UUID()
    weak var board: SessionKeyBoard?

    deinit {
        let id = id
        let board = board
        Task { @MainActor in board?.remove(id) }
    }
}

struct SessionKeysRegistration: ViewModifier {
    private struct Registration: Equatable {
        let keys: SessionKeyActions
        let page: StackEntry?
    }

    let keys: SessionKeyActions
    @Environment(AppNavigation.self) private var navigation: AppNavigation?
    @Environment(\.ownerTab) private var ownerTab
    @Environment(\.stackEntry) private var page
    @State private var token = SessionKeyToken()

    func body(content: Content) -> some View {
        content.onChange(of: Registration(keys: keys, page: page), initial: true) { _, registration in
            guard let navigation else { return }
            token.board = navigation.menu.keys
            navigation.menu.keys.set(registration.keys, id: token.id, tab: ownerTab ?? navigation.tab, page: registration.page)
        }
    }
}

extension View {
    func sessionKeys(scope: String, enabled: Bool, pending: PermissionRequest?, answer: @escaping (PermissionRequest, String) -> Void, confirmAlways: @escaping (String) -> Void, send: (() -> Void)?, stop: (() -> Void)? = nil, panes: PaneKeys? = nil, help: (() -> Void)? = nil) -> some View {
        modifier(SessionKeysModifier(scope: scope, enabled: enabled, pending: pending, answer: answer, confirmAlways: confirmAlways, send: send, stop: stop, panes: panes, help: help))
    }
}

struct SessionKeysModifier: ViewModifier {
    let scope: String
    let enabled: Bool
    let pending: PermissionRequest?
    let answer: (PermissionRequest, String) -> Void
    let confirmAlways: (String) -> Void
    let send: (() -> Void)?
    let stop: (() -> Void)?
    let panes: PaneKeys?
    let help: (() -> Void)?
    @State private var answered: String?
    @State private var armed: String?

    func body(content: Content) -> some View {
        content
            .modifier(SessionKeysRegistration(keys: SessionKeyActions(scope: scope + "|" + (pending?.id ?? ""), enabled: enabled, answers: answers, send: send.map { KeyedAction(label: "Send", run: $0) }, stop: stop.map { KeyedAction(label: "Stop", run: $0) }, panes: panes, help: help.map { KeyedAction(label: "Transcript Keys", run: $0) })))
            .task(id: pending?.id) {
                armed = nil
                try? await Task.sleep(for: .milliseconds(600))
                armed = pending?.id
            }
    }

    private var answers: [Int: KeyedAction] {
        guard let pending, pending.id != answered, pending.id == armed else { return [:] }
        var out: [Int: KeyedAction] = [:]
        for digit in 1...PermissionRequest.keyCount {
            guard let option = pending.keyed(digit) else { continue }
            out[digit] = KeyedAction(label: option.label) {
                if option.kind == .allowAlways {
                    confirmAlways(option.id)
                } else {
                    answered = pending.id
                    answer(pending, option.id)
                }
            }
        }
        return out
    }
}

struct SessionCommands: Commands {
    let menu: MenuBoard

    var body: some Commands {
        CommandMenu("Session") {
            Button(keys?.send?.label ?? "Send") {
                guard let live = menu.live.keys, live.enabled else { return }
                live.send?.run()
            }
            .keyboardShortcut(.return, modifiers: .command)
            .disabled(keys?.enabled != true || keys?.send == nil)
            if let keys {
                ForEach(1...PermissionRequest.keyCount, id: \.self) { digit in
                    if let action = keys.answers[digit] {
                        Button(action.label) {
                            guard let live = menu.live.keys, live.enabled else { return }
                            live.answers[digit]?.run()
                        }
                        .keyboardShortcut(KeyEquivalent(Character(String(digit))), modifiers: .option)
                        .disabled(!keys.enabled)
                    }
                }
            }
            Divider()
            Button("Stop") {
                guard let live = menu.live.keys, live.enabled else { return }
                live.stop?.run()
            }
            .keyboardShortcut(".", modifiers: .command)
            .disabled(keys?.enabled != true || keys?.stop == nil)
            Divider()
            paneButton("Go to List", .left, .leftArrow)
            paneButton("Go to Session", .right, .rightArrow)
            paneButton("Go to Transcript", .up, .upArrow)
            paneButton("Go to Composer", .down, .downArrow)
            ForEach(PaneLadder.controlKeys.sorted { $0.key > $1.key }, id: \.key) { key, arrow in
                paneButton(Self.alternate[arrow] ?? "", arrow, KeyEquivalent(Character(key)), modifiers: .control)
            }
            Divider()
            Button("Transcript Keys") {
                menu.live.keys?.help?.run()
            }
            .keyboardShortcut("/", modifiers: .command)
            .disabled(keys?.help == nil)
        }
    }

    private static let alternate: [PaneArrow: String] = [.left: "Go to List (Alternate)", .right: "Go to Session (Alternate)", .up: "Go to Transcript (Alternate)", .down: "Go to Composer (Alternate)"]

    private func paneButton(_ title: String, _ arrow: PaneArrow, _ key: KeyEquivalent, modifiers: EventModifiers = [.option, .command]) -> some View {
        Button(title) {
            menu.live.keys?.panes?.go(arrow)
        }
        .keyboardShortcut(key, modifiers: modifiers)
        .disabled(keys?.pane(arrow) == nil)
    }

    private var keys: SessionKeyActions? {
        menu.state.keys
    }
}
