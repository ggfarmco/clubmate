import Foundation

import ClubmateProtocol

public struct ShownSection: Sendable, Equatable, Identifiable {
    public let section: ListSection
    public let rows: [ListRow]

    public var id: String { section.id }
}

extension AgentsStore {
    public var treeMode: Bool {
        AgentsTree.supported(sessions) || olderShown
    }

    var olderRows: [AgentSession] {
        departed + older.filter { row in !departed.contains { $0.id == row.id } }
    }

    public var listCounts: [ListFilter: Int] {
        AgentsTree.counts(sessions)
    }

    public var shownQueues: [LandQueue] {
        listFilter == .all ? landQueues.filter(\.grouped) : []
    }

    public var shownSections: [ShownSection] {
        let latest = Dictionary(sessions.map { ($0.id, $0) }, uniquingKeysWith: { $1 })
        return AgentsTree.sections(hold.shown ?? sessions, filter: listFilter, older: olderRows, olderCount: olderShown ? olderCount : 0, inLine: landQueues.inLine).map { section in
            ShownSection(section: section, rows: section.rows(expanded: expanded, folded: foldedTasks, unfolded: unfoldedSections).map { $0.restyled(latest) })
        }
    }

    public func landList() {
        if hold.held || hold.shown != sessions { hold.land(sessions) }
        pruneDeparted()
    }

    @discardableResult
    func landAwaited() -> Bool {
        guard let awaited, sessions.contains(where: { $0.id == awaited }) else { return false }
        self.awaited = nil
        hold.land(sessions)
        return true
    }

    func offerList() {
        if landAwaited() { return }
        guard treeMode, fetched else { return hold.land(sessions) }
        let older = olderRows
        let olderCount = olderShown ? olderCount : 0
        let inLine = landQueues.inLine
        hold.offer(sessions, layout: { [listFilter, expanded, foldedTasks, unfoldedSections] sessions in
            let sections = AgentsTree.sections(sessions, filter: listFilter, older: older, olderCount: olderCount, inLine: inLine)
            return (sections, AgentsTree.layout(sections, expanded: expanded, folded: foldedTasks, unfolded: unfoldedSections))
        }, visible: visibleRows, onScreen: listOnScreen)
    }
}

extension ListRow {
    func restyled(_ latest: [String: AgentSession]) -> ListRow {
        var row = self
        var item = SessionRowItem(node: item.node.restyled(latest), depth: item.depth, expanded: item.expanded, hidden: item.hidden, repo: item.repo)
        item.taskRow = self.item.taskRow
        row.item = item
        return row
    }
}
