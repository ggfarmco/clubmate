import Foundation
import XCTest

@testable import CompanionKit

@MainActor
final class AgentsTreeTests: XCTestCase {
    private static let tree = #"""
    [
      {"id":"m1","worktree":"/r/m","status":"done","group":"done","listKey":"t1 m1","listGroupKey":"0","listGroupLabel":"main","listFilters":["all"]},
      {"id":"b1","worktree":"/r/b","status":"done","group":"open","openCount":2,"listOpen":3,"listKey":"t2 b1","listGroupKey":"1t2 /r/b","listGroupLabel":"chore","listFilters":["all","open","running"],"inboxKey":"1 t2 b1"},
      {"id":"b2","worktree":"/r/b","parentSession":"b1","status":"running","group":"running","openCount":1,"listKey":"t4 b2","listGroupKey":"1t2 /r/b","listGroupLabel":"chore","inboxKey":"0 t1 b2"},
      {"id":"m2","worktree":"/r/m","status":"waiting","group":"needsYou","needsYou":true,"listKey":"t3 m2","listGroupKey":"0","listGroupLabel":"main","listFilters":["all","needs"],"inboxKey":"0 t5 m2"},
      {"id":"m0","worktree":"/r/m","status":"done","asks":true,"group":"needsYou","needsYou":true,"listKey":"t0 m0","listGroupKey":"0","listGroupLabel":"main","listFilters":["all","needs"],"inboxKey":"0 t6 m0"},
      {"id":"p1","worktree":"/r/m","status":"error","group":"paused","paused":true,"pausedUntil":1800000000,"listKey":"t7 p1","listGroupKey":"0","listGroupLabel":"main","listFilters":["all"]},
      {"id":"c1","worktree":"/r/m","status":"closed","group":"closed","listKey":"t8 c1","listGroupKey":"0","listGroupLabel":"main"},
      {"id":"s1","worktree":"/r/m","status":"waiting","service":true,"group":"needsYou","needsYou":true,"listKey":"t9 s1","listGroupKey":"0","listGroupLabel":"main","listFilters":["needs"]}
    ]
    """#

    private func sessions(_ json: String = tree) throws -> [AgentSession] {
        try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8))
    }

    func testInboxMirrorsSitOverWorktreeGroupsInThePluginsOrder() throws {
        let sections = AgentsTree.sections(try sessions())
        XCTAssertEqual(sections.map(\.id), ["inbox", "inbox-open", "paused-1800000000", "wt:0", "wt:1t2 /r/b", "closed", "service"])
        XCTAssertEqual(sections.map { $0.nodes.map(\.id) }, [["b2", "m2", "m0"], ["b1"], ["p1"], ["m0", "m1", "m2"], ["b1"], ["c1"], ["s1"]])
        XCTAssertEqual(sections.map(\.title), ["Needs you", "Open", "Paused", "main", "chore", "Closed", "Service"])
        XCTAssertEqual(sections[0].rows(expanded: [], folded: [], unfolded: []).map { [$0.id, $0.place] }, [["inbox|b2", "chore"], ["inbox|m2", "main"], ["inbox|m0", "main"]])
        XCTAssertEqual(sections[4].rows(expanded: ["b1"], folded: [], unfolded: []).map(\.id), ["wt:1t2 /r/b|b1", "wt:1t2 /r/b|b2"])
        XCTAssertEqual(sections[5].rows(expanded: [], folded: [], unfolded: []), [])
        XCTAssertEqual(sections[5].rows(expanded: [], folded: [], unfolded: ["closed"]).map(\.session), ["c1"])
    }

    func testChipsFilterRootsAndCountWhatThePluginSent() throws {
        let all = try sessions()
        XCTAssertEqual(AgentsTree.counts(all), [.all: 5, .needs: 3, .open: 3, .running: 1])
        let running = AgentsTree.sections(all, filter: .running)
        XCTAssertEqual(running.map(\.id), ["wt:1t2 /r/b"])
        let needs = AgentsTree.sections(all, filter: .needs)
        XCTAssertEqual(needs.map(\.id), ["wt:0", "service"])
        XCTAssertEqual(needs[0].nodes.map(\.id), ["m2", "m0"])
        XCTAssertNil(needs[0].title)
    }

    func testAGateWaitingForTrustIsListedOnlyInTheInbox() throws {
        let gate = #"{"id":"gt_1","status":"waiting","kind":"gate","group":"needsYou","needsYou":true,"rowDetail":"Graph changed · 1 waits","listKey":"t9 gt_1","listFilters":["needs"],"inboxKey":"0 t9 gt_1"}"#
        let all = try sessions(String(Self.tree.dropLast()) + ",\(gate)]")
        XCTAssertEqual(AgentsTree.sections(all).filter { $0.nodes.contains { $0.id == "gt_1" } }.map(\.id), ["inbox"])
        XCTAssertEqual(AgentsTree.sections(all, filter: .needs).flatMap(\.nodes).filter { $0.id == "gt_1" }.count, 1)
    }

    func testLoneHomeGroupIsHeadedOnlyUnderAnInbox() throws {
        let home = #"{"id":"m1","status":"done","group":"done","listKey":"t1 m1","listGroupKey":"0","listGroupLabel":"main","listFilters":["all"]}"#
        let open = #"{"id":"m2","status":"done","group":"open","openCount":1,"listOpen":1,"listKey":"t2 m2","listGroupKey":"0","listGroupLabel":"main","listFilters":["all","open"],"inboxKey":"1 t2 m2"}"#
        XCTAssertEqual(AgentsTree.sections(try sessions("[\(home)]")).map(\.title), [nil])
        XCTAssertEqual(AgentsTree.sections(try sessions("[\(home),\(open)]")).map(\.title), ["Open", "main"])
    }

    func testTreeWaitsForTheInboxWhileSomethingNeedsYou() throws {
        let old = try sessions(#"[{"id":"a","status":"waiting","group":"needsYou","needsYou":true,"listFilters":["all","needs"]}]"#)
        XCTAssertFalse(AgentsTree.supported(old))
        XCTAssertTrue(AgentsTree.supported(try sessions(#"[{"id":"a","status":"done","group":"done","listFilters":["all"]}]"#)))
        XCTAssertTrue(AgentsTree.supported(try sessions()))
        XCTAssertFalse(AgentsTree.supported(try sessions(#"[{"id":"a","status":"done","group":"done"}]"#)))
    }

    private static let before = #"""
    [
      {"id":"m1","status":"done","group":"done","listKey":"t1 m1","listGroupKey":"0","listFilters":["all"]},
      {"id":"m2","status":"running","group":"running","listKey":"t2 m2","listGroupKey":"0","listFilters":["all","running"]},
      {"id":"m3","status":"done","group":"done","listKey":"t3 m3","listGroupKey":"0","listFilters":["all"]}
    ]
    """#

    private static let after = #"""
    [
      {"id":"m1","status":"done","group":"done","listKey":"t1 m1","listGroupKey":"0","listFilters":["all"]},
      {"id":"m2","status":"waiting","group":"needsYou","needsYou":true,"listKey":"t2 m2","listGroupKey":"0","listFilters":["all","needs"],"inboxKey":"0 m2"},
      {"id":"m3","status":"closed","group":"closed","listKey":"t3 m3","listGroupKey":"0"},
      {"id":"m4","status":"running","group":"running","listKey":"t4 m4","listGroupKey":"0","listFilters":["all","running"]}
    ]
    """#

    private func layout(_ sessions: [AgentSession]) -> (sections: [ListSection], entries: [LayoutEntry]) {
        let sections = AgentsTree.sections(sessions)
        return (sections, AgentsTree.layout(sections, expanded: [], folded: [], unfolded: []))
    }

    func testAChangeThatMovesAVisibleRowWaitsAndNamesWhatWaits() throws {
        var hold = ListHold()
        hold.land(try sessions(Self.before))
        hold.offer(try sessions(Self.after), layout: layout, visible: ["wt:0|m1", "wt:0|m2", "wt:0|m3"], onScreen: true)
        XCTAssertEqual(hold.shown?.map(\.status), [.done, .running, .done])
        XCTAssertEqual(hold.pill, "1 needs you · 1 new · 1 moved")
        hold.land(try sessions(Self.after))
        XCTAssertNil(hold.pill)
        XCTAssertEqual(hold.shown?.map(\.id), ["m1", "m2", "m3", "m4"])
    }

    func testChangesOffScreenOrBelowTheLastVisibleRowLandAtOnce() throws {
        let appended = try sessions(Self.before.replacingOccurrences(of: "\n]", with: #",{"id":"m4","status":"running","group":"running","listKey":"t4 m4","listGroupKey":"0","listFilters":["all","running"]}]"#))
        var hold = ListHold()
        hold.land(try sessions(Self.before))
        hold.offer(appended, layout: layout, visible: ["wt:0|m1", "wt:0|m2", "wt:0|m3"], onScreen: true)
        XCTAssertNil(hold.pill)
        XCTAssertEqual(hold.shown?.count, 4)
        hold.land(try sessions(Self.before))
        hold.offer(try sessions(Self.after), layout: layout, visible: ["wt:0|m1"], onScreen: false)
        XCTAssertNil(hold.pill)
        XCTAssertEqual(hold.shown?.count, 4)
        let needsChip = { (sessions: [AgentSession]) -> (sections: [ListSection], entries: [LayoutEntry]) in
            let sections = AgentsTree.sections(sessions, filter: .needs)
            return (sections, AgentsTree.layout(sections, expanded: [], folded: [], unfolded: []))
        }
        hold.land(try sessions(Self.before))
        hold.offer(try sessions(Self.after), layout: needsChip, visible: ["wt:0|m1"], onScreen: true)
        XCTAssertNil(hold.pill)
        XCTAssertEqual(hold.shown?.count, 4)
    }

    private static func page(_ json: String, delta: Bool) -> String {
        #"{"sessions":\#(json),"removed":[],"cursor":"e.\#(delta ? 2 : 1)"\#(delta ? #","delta":true"# : "")}"#
    }

    func testASessionTheUserStartedLandsWhicheverArrivesFirst() async throws {
        let started = Self.before.replacingOccurrences(of: "\n]", with: #",{"id":"m0","status":"running","group":"running","listKey":"t0 m0","listGroupKey":"0","listFilters":["all","running"]}]"#)
        let landed = Landed(item: QueuedAction(computer: "c", workspace: "w", command: AgentCommand.new, args: .object([:]), target: "", title: "New session"), result: .object(["session": .string("m0")]), at: .now)
        for landedFirst in [true, false] {
            let runner = ScriptedRunner(replies: [], sessions: [Self.page(Self.before, delta: false), Self.page(started, delta: true)])
            let store = AgentsStore(workspace: "w", runner: runner)
            await store.refresh()
            store.listOnScreen = true
            store.visibleRows = ["wt:0|m1", "wt:0|m2", "wt:0|m3"]
            if landedFirst { store.landed(landed) }
            await store.refresh()
            if !landedFirst {
                XCTAssertEqual(store.hold.pill, "1 new")
                store.landed(landed)
            }
            XCTAssertNil(store.hold.pill)
            XCTAssertEqual(store.shownSections.flatMap(\.rows).first?.session, "m0")
        }
    }

    func testAHeldRowStillShowsItsNewStateInPlace() async throws {
        let runner = ScriptedRunner(replies: [], sessions: [Self.page(Self.before, delta: false), Self.page(Self.after, delta: true)])
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.refresh()
        store.listOnScreen = true
        store.visibleRows = ["wt:0|m1", "wt:0|m2", "wt:0|m3"]
        await store.refresh()
        XCTAssertEqual(store.hold.pill, "1 needs you · 1 new · 1 moved")
        let rows = store.shownSections.flatMap(\.rows)
        XCTAssertEqual(rows.map(\.id), ["wt:0|m1", "wt:0|m2", "wt:0|m3"])
        XCTAssertEqual(rows.map(\.item.node.session.glyph), [.done, .permission, .closed])
        XCTAssertEqual(store.listCounts[.needs], 1)
        store.landList()
        XCTAssertEqual(store.shownSections.map(\.id), ["inbox", "wt:0", "closed"])
    }
}
