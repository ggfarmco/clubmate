import Foundation
import XCTest

import CompanionKit

final class TabNavigationTests: XCTestCase {
    private func stacks(_ tab: CompanionTab = .agents) -> TabStacks {
        var stacks = TabStacks()
        stacks.select(computer: "mac1", workspace: "shop")
        stacks.tab = tab
        return stacks
    }

    private func defaults() throws -> UserDefaults {
        try XCTUnwrap(UserDefaults(suiteName: UUID().uuidString))
    }

    func testCrossSectionPushShowsChipWithOriginTitle() {
        var stacks = stacks()
        stacks.push(.session("a"))
        stacks.push(.task(4), from: Origin(tab: .agents, title: "Fix login"))
        stacks.tab = .backlog
        stacks.push(.task(4))
        stacks.push(.session("a"), from: Origin(tab: .backlog, title: "#4"))
        XCTAssertEqual(stacks.backControl(at: 1), .chip("#4"))
        stacks.push(.doc(path: "/a.md", anchor: nil), from: Origin(tab: .agents, title: "Fix login"))
        XCTAssertEqual(stacks.backControl(at: 2), .chip("Fix login"))
        stacks.tab = .agents
        XCTAssertEqual(stacks.backControl(at: 1), .chip("Fix login"))
        stacks.tab = .backlog
        XCTAssertEqual(stacks.sectionLine(at: 1, workspace: "shop"), "Agents · shop")
        XCTAssertNil(stacks.sectionLine(at: 0, workspace: "shop"))
    }

    func testSameSectionPushShowsChevron() {
        var stacks = stacks(.backlog)
        stacks.push(.task(4))
        stacks.push(.task(7), from: Origin(tab: .backlog, title: "#4"))
        stacks.push(.doc(path: "/a.md", anchor: nil), from: Origin(tab: .backlog, title: "#7"))
        stacks.push(.doc(path: "/b.md", anchor: nil), from: Origin(tab: nil, title: "a.md"))
        XCTAssertEqual((0..<4).map { stacks.backControl(at: $0) }, [.chevron, .chevron, .chip("#7"), .chevron])
    }

    func testInboxAndNotificationOriginsShowChip() {
        var stacks = stacks()
        stacks.push(.session("a"), from: Origin(tab: .agents, title: "Inbox", kind: .inbox))
        stacks.push(.session("b"), from: Origin(tab: .agents, title: "Fix login", kind: .notification))
        XCTAssertEqual([stacks.backControl(at: 0), stacks.backControl(at: 1)], [.chip("Inbox"), .chip("Fix login")])
    }

    func testChipLabelCutAt12() {
        var stacks = stacks(.backlog)
        stacks.push(.task(4), from: Origin(tab: .agents, title: "Refactor the cart total"))
        stacks.push(.task(7), from: Origin(tab: .agents, title: "Exactly twel"))
        XCTAssertEqual([stacks.backControl(at: 0), stacks.backControl(at: 1)], [.chip("Refactor th…"), .chip("Exactly twel")])
    }

    func testWorkspaceSwitchRestoresEveryTabStack() throws {
        var stacks = stacks(.wiki)
        stacks.push(.wikiPage(id: "p1", path: "a.md", title: "A"))
        stacks.tab = .backlog
        stacks.push(.task(4))
        stacks.select(computer: "mac2", workspace: "api")
        XCTAssertEqual(stacks.tab, .agents)
        XCTAssertEqual(stacks.path, [])
        stacks.push(.session("x"))
        let defaults = try defaults()
        stacks.save(to: defaults)
        var restored = TabStacks.restore(from: defaults)
        XCTAssertEqual(restored, stacks)
        restored.select(computer: "mac1", workspace: "shop")
        XCTAssertEqual(restored.tab, .backlog)
        XCTAssertEqual(restored.path, [StackEntry(.task(4))])
        restored.tab = .wiki
        XCTAssertEqual(restored.path, [StackEntry(.wikiPage(id: "p1", path: "a.md", title: "A"))])
    }

    func testRestoreDropsGoneEntries() throws {
        var stacks = stacks()
        let api = WorkspaceKey(computer: "mac1", workspace: "api")
        stacks.push(.session("a"))
        stacks.push(.session("closed"))
        stacks.push(.task(4), from: Origin(tab: .agents, title: "closed"))
        stacks.tab = .backlog
        stacks.push(.task(4))
        stacks.push(.session("a"), from: Origin(tab: .backlog, title: "#4"), in: api)
        let defaults = try defaults()
        stacks.save(to: defaults)
        var restored = TabStacks.restore(from: defaults) { workspace, destination in
            destination == .session("closed") || workspace == api && destination == .session("a")
        }
        XCTAssertEqual(restored.path, [StackEntry(.task(4))])
        restored.tab = .agents
        XCTAssertEqual(restored.path, [StackEntry(.session("a"))])
    }

    func testDocsTabRestoresAsAgents() throws {
        let defaults = try defaults()
        let key = #"{"computer":"mac1","workspace":"shop","tab":"%@"}"#
        let saved = """
        {"version":1,"workspace":{"computer":"mac1","workspace":"shop"},"tabs":[{"workspace":{"computer":"mac1","workspace":"shop"},"tab":"docs"}],
         "stacks":[{"key":\(String(format: key, "docs")),"entries":[{"destination":{"doc":{"path":"/a.md"}}}]},
                   {"key":\(String(format: key, "agents")),"entries":[{"destination":{"session":{"_0":"a"}}},{"destination":{"future":{}}}]}],
         "columns":[]}
        """
        defaults.set(Data(saved.utf8), forKey: TabStacks.defaultsKey)
        let restored = TabStacks.restore(from: defaults)
        XCTAssertEqual(restored.tab, .agents)
        XCTAssertEqual(restored.path, [StackEntry(.session("a"))])
        defaults.set(Data(#"{"version":2}"#.utf8), forKey: TabStacks.defaultsKey)
        XCTAssertEqual(TabStacks.restore(from: defaults), TabStacks())
    }

    func testReplaceTopKeepsOriginalOrigin() {
        var stacks = stacks(.backlog)
        let origin = Origin(tab: .agents, title: "Inbox", kind: .inbox)
        stacks.push(.session("a"), from: origin)
        let api = WorkspaceKey(computer: "mac2", workspace: "api")
        stacks.replaceTop(.session("b"), in: api)
        XCTAssertEqual(stacks.path, [StackEntry(.session("b"), origin: origin, workspace: api)])
    }

    func testTabsShownDependOnWiki() {
        XCTAssertEqual(CompanionTab.shown(hasWiki: false), [.agents, .backlog, .more])
        XCTAssertEqual(CompanionTab.shown(hasWiki: true), [.agents, .backlog, .wiki, .more])
    }

    func testKnownTabsHoldUntilSectionsLoadAndSurviveALaunch() throws {
        let shop = WorkspaceKey(computer: "studio", workspace: "shop")
        let infra = WorkspaceKey(computer: "pro", workspace: "infra")
        let fresh = WorkspaceKey(computer: "studio", workspace: "clubmate")
        let full = CompanionTab.shown(hasWiki: true)
        let noWiki = CompanionTab.shown(hasWiki: false)
        var known = KnownTabs()
        XCTAssertNil(known.tabs(for: shop, live: nil))
        known.remember(full, for: shop)
        known.remember(noWiki, for: infra)
        XCTAssertEqual(known.tabs(for: shop, live: nil), full)
        XCTAssertEqual(known.tabs(for: fresh, live: nil), noWiki)
        XCTAssertEqual(known.tabs(for: shop, live: [.agents]), [.agents])

        let defaults = try defaults()
        known.save(to: defaults)
        let launched = KnownTabs.restore(from: defaults)
        XCTAssertEqual(launched.tabs(for: shop, live: nil), full)
        XCTAssertEqual(launched, known)
    }
}
