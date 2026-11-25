import SwiftUI
import UIKit

import CompanionKit

struct MenuState: Equatable {
    var createTitle = "New"
    var canCreate = false
    var wide = false
    var listShown = false
    var readingView = false
    var canRead = false
    var canChat = false
    var chatShown = false
    var canEscape = false
    var listStop = false
    var canGoBack = false
    var shownTabs: [CompanionTab] = []
    var hasComputer = false
    var tabHasSearch = false
    var keys: SessionKeyActions?

    var enabled: Set<String> {
        var items: Set<String> = []
        if canCreate { items.insert("create") }
        if wide { items.insert("list") }
        if readingView || canRead { items.insert("read") }
        if chatShown || canChat { items.insert("chat") }
        if canEscape || listStop { items.insert("escape") }
        if canGoBack { items.insert("back") }
        if hasComputer { items.insert("inbox") }
        if tabHasSearch { items.insert("search") }
        items.formUnion(shownTabs.map { "tab." + $0.rawValue })
        if let keys {
            for arrow in [PaneArrow.left, .right, .up, .down] where keys.pane(arrow) != nil { items.insert("pane.\(arrow)") }
        }
        if let keys, keys.enabled {
            if keys.send != nil { items.insert("send") }
            if keys.stop != nil { items.insert("stop") }
            items.formUnion(keys.answers.keys.map { "answer.\($0)" })
        }
        return items
    }
}

private struct PaneScope: Equatable {
    let tab: CompanionTab
    let depth: Int
    let compact: Bool
}

struct MenuMove: Equatable {
    let tab: CompanionTab
    let depth: Int
}

@MainActor
@Observable
final class MenuBoard {
    private(set) var state = MenuState()
    @ObservationIgnored private(set) var live = MenuState()
    @ObservationIgnored let keys = SessionKeyBoard()
    @ObservationIgnored let panes = PaneFocus()
    @ObservationIgnored private var pending: MenuState?
    @ObservationIgnored private var move: MenuMove?
    @ObservationIgnored private var settling: Task<Void, Never>?

    func offer(_ next: MenuState, move: MenuMove) {
        let moved = self.move != nil && self.move != move
        self.move = move
        live = next
        pending = next
        if !next.enabled.isSubset(of: state.enabled) {
            settling?.cancel()
            publish()
        } else if moved {
            settling?.cancel()
            settling = Task { [weak self] in
                try? await Task.sleep(for: Motion.navigationSettle)
                guard !Task.isCancelled else { return }
                self?.publish()
            }
        } else if settling == nil {
            publish()
        }
    }

    private func publish() {
        settling = nil
        guard let pending else { return }
        self.pending = nil
        guard pending != state else { return }
        state = pending
        UIMenuSystem.main.setNeedsRebuild()
    }
}

struct MenuFeed: View {
    let board: MenuBoard
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        Color.clear
            .onChange(of: navigation.selected(.agents), initial: true) { _, id in navigation.noteSession(id) }
            .onChange(of: state, initial: true) { _, state in
                board.offer(state, move: MenuMove(tab: navigation.tab, depth: navigation.path.count))
            }
            .onChange(of: PaneScope(tab: navigation.tab, depth: navigation.path.count, compact: navigation.layout == .compact)) {
                board.panes.listFocused = false
            }
    }

    private var state: MenuState {
        MenuState(
            createTitle: navigation.createTitle,
            canCreate: navigation.canCreate(model),
            wide: navigation.layout != .compact,
            listShown: navigation.listShown,
            readingView: navigation.readingView,
            canRead: navigation.canRead,
            canChat: navigation.canChat,
            chatShown: navigation.chatShown,
            canEscape: navigation.canEscape,
            listStop: navigation.menu.panes.listFocused && board.keys.keys(for: navigation.listTab, top: navigation.path.last)?.panes != nil,
            canGoBack: navigation.canGoBack,
            shownTabs: CompanionTab.keyed.filter { navigation.shows($0, model) },
            hasComputer: navigation.computer != nil,
            tabHasSearch: navigation.tabHasSearch,
            keys: board.keys.keys(for: navigation.listTab, top: navigation.path.last)
        )
    }
}
