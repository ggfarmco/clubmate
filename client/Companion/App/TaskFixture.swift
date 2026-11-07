#if DEBUG
import Foundation

import CompanionKit

enum TaskFixture {
    static let owner = "agt_0000000000000031"
    static let management = "agt_0000000000000032"
    static let snapshot = "agt_0000000000000033"
    static let auth = "agt_0000000000000034"
    static let bump = "agt_0000000000000035"
    static let request = "cr_fixture"
    static let land = "lr_fixture"
    static let plan = "plan_fixture"

    static var scene: String? {
        UserDefaults.standard.string(forKey: "taskFixture")
    }

    static var folders: [JSONValue] {
        [.object(["path": .string("/Users/me/src/ads"), "seen": .bool(true)]), .object(["path": .string("/Users/me/src/internal-auth"), "seen": .bool(scene != "afk")])]
    }

    static func sessions(_ template: [String: JSONValue], stamp: (TimeInterval) -> JSONValue) -> [[String: JSONValue]] {
        let scene = scene ?? ""
        func kid(_ id: String, _ repo: String, status: String, activity: String?) -> [String: JSONValue] {
            var fields = template
            for key in ["pending", "pendingSummary", "question", "needsYou", "needsYouSince"] { fields[key] = nil }
            fields["id"] = .string(id)
            fields["title"] = .string(repo + " work")
            fields["repo"] = .string(repo)
            fields["parentSession"] = .string(owner)
            fields["status"] = .string(status)
            fields["asks"] = .bool(false)
            fields["group"] = .string(status == "running" ? "running" : "done")
            fields["createdAt"] = stamp(240)
            fields["updatedAt"] = stamp(status == "running" ? 5 : 60)
            if status == "running" { fields["turnStartedAt"] = stamp(180) }
            if let activity { fields["activity"] = .object(["title": .string(activity), "at": stamp(180)]) }
            return fields
        }
        var top = template
        for key in ["pending", "pendingSummary", "question", "needsYou", "needsYouSince"] { top[key] = nil }
        top["id"] = .string(owner)
        top["worktree"] = .string("/Users/me/src/ads")
        top["title"] = .string("Campaign pause reason")
        top["status"] = .string("done")
        top["asks"] = .bool(false)
        top["createdAt"] = stamp(600)
        top["updatedAt"] = stamp(120)
        top["group"] = .string("done")
        func needs(_ pending: [String], _ summary: [String: JSONValue]) {
            var summary = summary
            summary["at"] = stamp(120)
            top["needsYou"] = .bool(true)
            top["needsYouSince"] = stamp(120)
            top["group"] = .string("needsYou")
            top["pending"] = .array(pending.map(JSONValue.string))
            top["requests"] = .array(pending.map(JSONValue.string))
            top["pendingSummary"] = .object(summary)
        }
        let row = { (name: String, session: String, word: String?) -> JSONValue in
            .object(["name": .string(name), "session": .string(session)].merging(word.map { ["word": .string($0)] } ?? [:]) { $1 })
        }
        var kids: [[String: JSONValue]]
        switch scene {
        case "connect", "connectMany", "workspace":
            needs([request], ["kind": .string("connect"), "text": .string("Connect internal-auth?"), "lead": .string("Wants to connect"), "subject": .string("internal-auth"), "onMac": .bool(true)])
            top["taskRows"] = .array([row("ads-management", management, nil), row("ads-snapshot", snapshot, nil), .object(["name": .string("internal-auth"), "path": .string("/Users/me/src/internal-auth"), "word": .string("not connected yet"), "tone": .string("warning"), "connect": .bool(true)])])
            kids = [kid(management, "ads-management", status: "running", activity: "Editing campaign/pause.go"), kid(snapshot, "ads-snapshot", status: "running", activity: "Running migrations")]
        case "land":
            needs([land], ["kind": .string("land"), "text": .string("Land internal-auth?"), "onMac": .bool(true)])
            top["taskBadge"] = .string("1 to land")
            top["taskRows"] = .array([row("ads-snapshot", snapshot, "merged"), row("internal-auth", auth, "asking"), row("ads-management", management, "waits")])
            kids = [kid(snapshot, "ads-snapshot", status: "done", activity: nil), kid(auth, "internal-auth", status: "done", activity: nil), kid(management, "ads-management", status: "done", activity: nil)]
        case "failed":
            needs([], ["kind": .string("failed"), "text": .string("internal-auth failed"), "onMac": .bool(true)])
            top["taskRows"] = .array([row("ads-snapshot", snapshot, "merged"), row("internal-auth", auth, "failed"), row("ads-management", management, "waits")])
            kids = [kid(snapshot, "ads-snapshot", status: "done", activity: nil), kid(auth, "internal-auth", status: "done", activity: nil), kid(management, "ads-management", status: "done", activity: nil)]
        default:
            top["taskBadge"] = .string("1 PR open")
            top["archivable"] = .bool(true)
            top["taskFinished"] = .bool(true)
            top["taskRows"] = .array([row("ads-snapshot", snapshot, "merged"), row("internal-auth", auth, "merged"), row("ads-management", management, "PR open")])
            kids = [kid(snapshot, "ads-snapshot", status: "done", activity: nil), kid(auth, "internal-auth", status: "done", activity: nil), kid(management, "ads-management", status: "done", activity: nil)]
        }
        var done = template
        for key in ["pending", "pendingSummary", "question", "needsYou", "needsYouSince"] { done[key] = nil }
        done["id"] = .string(bump)
        done["title"] = .string("Bump the SDK in ads-management")
        done["status"] = .string("done")
        done["asks"] = .bool(false)
        done["group"] = .string("done")
        done["createdAt"] = stamp(7_200)
        done["updatedAt"] = stamp(3_700)
        return [top] + kids + [done]
    }

    static func transcript(entry: (String, String, String) -> JSONValue) -> [String: JSONValue] {
        let scene = scene ?? ""
        var out: [String: JSONValue] = [:]
        switch scene {
        case "connect", "workspace":
            out["entries"] = .array([entry("agent", "task-1", "ads-management and ads-snapshot are running. internal-auth isn't in this workspace yet.")])
            out["request"] = connectNode(many: false)
        case "connectMany":
            out["entries"] = .array([entry("agent", "task-1", "I found three repos this task needs.")])
            out["request"] = connectNode(many: true)
        case "land":
            out["entries"] = .array([entry("agent", "task-1", "All three children are done. Finish mode is **ask before merging**: internal-auth is next in order and waits for you.")])
            out["request"] = landNode
            out["repos"] = reposNode(failed: false, archive: false)
        case "failed":
            out["entries"] = .array([entry("agent", "task-1", "internal-auth failed its tests, so ads-management is held.")])
            out["repos"] = reposNode(failed: true, archive: false)
        case "afk":
            out["entries"] = .array([entry("agent", "task-1", "All done."), .object(["kind": .string("notice"), "id": .string("afk-1"), "turn": .number(1), "notice": .object(["title": .string("Connected internal-auth while you were away"), "description": .string("/Users/me/src/internal-auth"), "repo": .string("internal-auth"), "path": .string("/Users/me/src/internal-auth")]), "afk": .object(["detail": .string("03:12 · a PR at most, never merges"), "remove": .string("Remove from ads…")])])])
            out["repos"] = reposNode(failed: false, archive: true)
        default:
            out["entries"] = .array([entry("agent", "task-1", "All three repos are settled.")])
            out["repos"] = reposNode(failed: false, archive: true)
        }
        return out
    }

    private static func connectNode(many: Bool) -> JSONValue {
        func repo(_ name: String, reason: String, state: String = "main · clean", warn: Bool = false, brings: String, folded: Bool = false) -> JSONValue {
            var fields: [String: JSONValue] = ["key": .string("/Users/me/src/" + name), "name": .string(name), "path": .string("~/develop/acme/" + name), "remote": .string("github.com/acme/" + name), "state": .string(state), "reason": .string(reason), "landsBy": .string("merge to main after tests"), "source": .string("from its CLAUDE.md"), "brings": .string(brings), "trust": .string("graph steps stay off until you trust them")]
            if warn { fields["warn"] = .bool(true) }
            if folded { fields["folded"] = .bool(true) }
            return .object(fields)
        }
        var props: [String: JSONValue] = ["request": .string(request), "connect": .string("Connect"), "seen": .string("seen-1")]
        if many {
            props["title"] = .string("Connect 4 repos?")
            props["caption"] = .string("Claude found folders this workspace doesn't have · asks for all of them at once")
            props["effect"] = .string("Each becomes a folder of **ads** and stays. Claude starts a child session in each, on a new branch, with that repo's own CLAUDE.md and memory; a repo's own rules win over the plan.")
            props["declined"] = .string("Un-ticked repos are skipped, not left pending. Don't connect: Claude goes on with ads-management only and says what is missing.")
            props["more"] = .string("1 more repo")
            props["repos"] = .array([
                repo("internal-auth", reason: "The pause endpoint needs a campaigns:pause scope.", brings: "CLAUDE.md · 1 graph **deploy**"),
                repo("ads-snapshot", reason: "It stores the campaign state.", state: "3 behind origin/main", warn: true, brings: "CLAUDE.md, 2 skills"),
                repo("campaign-contracts-with-a-very-long-name-for-stress", reason: "The pause reason is in its schema.", brings: "no instructions, graphs or hooks"),
                repo("ads-admin", reason: "Its screens show the reason.", brings: "CLAUDE.md", folded: true),
            ])
        } else {
            props["title"] = .string("Connect a repo?")
            props["caption"] = .string("Claude wants a folder this workspace doesn't have")
            props["effects"] = .array([
                .object(["text": .string("Becomes a folder of **ads**"), "note": .string("stays after the task · remove it any time")]),
                .object(["text": .string("Claude starts a child session there"), "note": .string("new branch `feat/campaign-pause-reason` · internal-auth's own CLAUDE.md and memory")]),
                .object(["text": .string("Lands by merge to main after tests"), "note": .string("from its CLAUDE.md · its rules win over the plan")]),
                .object(["text": .string("Brings 1 graph and 1 worktree hook"), "note": .string("graph steps and the hook stay off until you trust them")]),
            ])
            props["repos"] = .array([repo("internal-auth", reason: "The pause endpoint needs a campaigns:pause scope, and internal-auth issues the scopes.", brings: "1 graph")])
        }
        return .object(["type": .string("request.connectRepo"), "props": .object(props)])
    }

    private static let landNode: JSONValue = .object(["type": .string("request.land"), "props": .object([
        "request": .string(land), "title": .string("Land internal-auth?"), "name": .string("internal-auth"),
        "detail": .string("Merge `feat/campaign-pause-reason` into `main` · 2 commits"), "rule": .string("By its rule: merge to main (its CLAUDE.md). ads-snapshot already landed; ads-management waits for this one."),
        "confirm": .string("Leave internal-auth unlanded? Leaving it unlanded holds ads-management too."),
        "options": .array([
            .object(["id": .string("review"), "label": .string("Review…")]),
            .object(["id": .string("merge"), "label": .string("Merge")]),
            .object(["id": .string("leave"), "label": .string("Leave it unlanded…")]),
        ]),
    ])])

    private static func reposNode(failed: Bool, archive: Bool) -> JSONValue {
        func button(_ label: String, _ action: String, confirm: String? = nil) -> JSONValue {
            var props: [String: JSONValue] = ["label": .string(label), "action": .string(action)]
            if let confirm { props["confirm"] = .string(confirm) }
            return .object(["type": .string("button"), "props": .object(props)])
        }
        func row(_ order: Int, _ name: String, _ session: String, _ state: String, tone: String? = nil, open: Bool = false, tail: String? = nil, actions: String? = nil) -> JSONValue {
            var fields: [String: JSONValue] = ["id": .string("row-\(order)"), "order": .number(Double(order)), "name": .string(name), "path": .string("/Users/me/src/" + name), "session": .string(session), "landsBy": .string("merge to main"), "state": .string(state), "opens": .bool(true), "foldKey": .string(state)]
            if let tone { fields["tone"] = .string(tone) }
            if open { fields["open"] = .bool(true) }
            if let tail { fields["tail"] = .string(tail) }
            if let actions { fields["actions"] = .string(actions) }
            return .object(fields)
        }
        var props: [String: JSONValue] = ["title": .string("3 repos"), "foldKey": .string(failed ? "failed" : archive ? "" : "land"), "action": .string("reposToggle"), "rowAction": .string("reposRow"), "footer": .string("Lands in order 1 → 2 → 3 · a repo waits until the ones before it landed"), "summary": .array([.object(["text": .string("1 merged")])])]
        var bars: [JSONValue] = []
        let auth = failed ? "row-2/actions" : "row-2/bar"
        props["rows"] = .array([
            row(1, "ads-snapshot", snapshot, "merged · PR #87", tone: "success"),
            failed ? row(2, "internal-auth", Self.auth, "failed", tone: "danger", open: true, tail: "--- FAIL: TestPauseScope (0.02s)\n    scope_test.go:41: want campaigns:pause\nFAIL\nexit status 1", actions: auth)
                : archive ? row(2, "internal-auth", Self.auth, "merged · PR #88", tone: "success") : row(2, "internal-auth", Self.auth, "ready to land", tone: "warning", actions: auth),
            archive ? row(3, "ads-management", management, "PR #412 open") : row(3, "ads-management", management, "on its branch · waits for 2"),
        ])
        if !archive {
            let buttons = (failed ? [button("Ask it to fix", "repoFix")] : []) + [button("Open its session", "repoSession"), button("Open in Code", "repoCode"), .object(["type": .string("spacer")]), button("Leave it unlanded…", "repoLeave", confirm: "Leave internal-auth unlanded? Its branch stays. Leaving it unlanded holds ads-management too.")]
            bars.append(.object(["type": .string("stack"), "id": .string(auth), "children": .array(buttons)]))
        } else {
            props["footer"] = .string("All children done · 3 worktrees left behind")
            props["footerAction"] = .string("repos/footer")
            bars.append(.object(["type": .string("stack"), "id": .string("repos/footer"), "children": .array([button("Archive task…", "archive")])]))
        }
        return .object(["type": .string("repos"), "props": .object(props), "children": .array(bars)])
    }

    static let commits: JSONValue = .object(["commits": .array([
        .object(["sha": .string("a1f4c2e"), "title": .string("Add the campaigns:pause scope")]),
        .object(["sha": .string("9bd03f1"), "title": .string("Issue it to the ads service account")]),
    ]), "tip": .string("a1f4c2e"), "detail": .string("Merge `feat/campaign-pause-reason` into `main` · 2 commits")])

    static let archivePlan: JSONValue = {
        func repo(_ name: String, _ tree: String, _ branch: String, why: String, tone: String? = nil, summary: String) -> JSONValue {
            var fields: [String: JSONValue] = ["name": .string(name), "path": .string("/Users/me/src/" + name), "worktree": .object(["action": .string(tree)]), "branch": .object(["action": .string(branch)]), "summary": .string(summary), "why": .string(why), "seen": .string("tip-" + name), "state": .object(["path": .string("/Users/me/src/" + name + "/.worktrees/campaign-pause-reason")])]
            if let tone { fields["whyTone"] = .string(tone) }
            return .object(fields)
        }
        return .object(["session": .string(owner), "title": .string("Campaign pause reason"), "workspace": .string("ads"), "sessions": .number(4), "seen": .string(plan), "blockers": .array([]), "holds": .array([]), "cronLines": .array([.string("nightly-sync runs in ads-snapshot ▸ campaign-pause-reason and pauses until you resume it in Cron.")]), "repos": .array([
            repo("ads-snapshot", "remove", "delete", why: "merged in PR #87 · local only", summary: "Worktree removed · branch deleted"),
            repo("internal-auth", "keep", "keep", why: "1 file would be lost — open it on the Mac", tone: "warning", summary: "Worktree kept · branch kept"),
            repo("ads-management", "remove", "keep", why: "PR #412 is open — the branch is the PR", summary: "Worktree removed · branch kept"),
        ])])
    }()
}
#endif
