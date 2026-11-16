#if DEBUG
import Foundation

import CompanionKit

enum LandReviewFixture {
    static let asking = "agt_00000000000000b1"
    static let askingReviewer = "agt_00000000000000b2"
    static let changes = "agt_00000000000000b3"
    static let changesReviewer = "agt_00000000000000b4"
    static let reviewing = "agt_00000000000000b5"
    static let reviewingReviewer = "agt_00000000000000b6"
    static let closedReviewer = "agt_00000000000000b7"
    static let approved = "agt_00000000000000b8"
    static let permission = "permission-r1"
    static let command = "go test ./plugins/gates/..."

    static var on: Bool {
        UserDefaults.standard.bool(forKey: "landReview")
    }

    static let section = JSONValue.object(["id": .string("agents.gates"), "title": .string("Land gates"), "icon": .string("checkmark.shield"), "order": .number(38), "scope": .string("workspace"), "autoNew": .bool(false), "plugin": .string("agents"), "rail": .bool(false)])

    static let dock: [JSONValue] = [
        .object(["id": .string("service"), "label": .string("Service"), "icon": .string("gearshape.2"), "detail": .string("0"), "phoneDetail": .string("0")]),
        .object(["id": .string("gates"), "label": .string("Land gates"), "icon": .string("checkmark.shield"), "detail": .string("1 asking"), "phoneDetail": .string("1 asking")]),
        .object(["id": .string("graphs"), "label": .string("Graphs"), "icon": .string("point.3.connected.trianglepath.dotted"), "detail": .string("0"), "phoneDetail": .string("0")]),
    ]

    static func sessions(_ template: [String: JSONValue], stamp: (TimeInterval) -> JSONValue) -> [[String: JSONValue]] {
        func record(_ id: String, _ title: String, status: String, group: String, ago: TimeInterval, _ extra: [String: JSONValue]) -> [String: JSONValue] {
            var fields = template
            for key in ["pending", "pendingSummary", "question", "parentSession", "needsYou", "needsYouSince", "activity", "turnStartedAt"] { fields[key] = nil }
            fields["id"] = .string(id)
            fields["title"] = .string(title)
            fields["status"] = .string(status)
            fields["group"] = .string(group)
            fields["asks"] = .bool(false)
            fields["worktree"] = .string("/Users/me/src/clubmate/.worktrees/feat/" + (branches[id] ?? "main"))
            fields["createdAt"] = stamp(ago + 1_800)
            fields["updatedAt"] = stamp(ago)
            return fields.merging(extra) { $1 }
        }
        func landing(_ verdict: String, round: Int, words: String, tone: String, badge: String, reviewer: String, findings: Int? = nil) -> JSONValue {
            var fields: [String: JSONValue] = ["round": .number(Double(round)), "of": .number(2), "verdict": .string(verdict), "words": .string(words), "tone": .string(tone), "badge": .string(badge), "into": .string("main")]
            if !reviewer.isEmpty { fields["reviewer"] = .string(reviewer) }
            if let findings { fields["findings"] = .number(Double(findings)) }
            return .object(fields)
        }
        func round(_ n: Int, _ verdict: String, label: String? = nil) -> JSONValue {
            .object(["round": .number(Double(n)), "label": .string(label ?? "Round \(n) review"), "verdict": .string(verdict)])
        }
        let branches = [asking: "gate-graph", askingReviewer: "gate-graph", changes: "keep-awake", changesReviewer: "keep-awake", reviewing: "side-threads", reviewingReviewer: "side-threads", closedReviewer: "side-threads", approved: "flaky-switcher"]
        let reviewer = { (lander: String) in JSONValue.object(["lander": .string(lander), "into": .string("main")]) }
        return [
            record(asking, "Gate graph instead of a fixed guard", status: "idle", group: "needsYou", ago: 120, [
                "childStatus": .string("waiting"), "landing": landing("reviewing", round: 1, words: "review…", tone: "accent", badge: "reviewing", reviewer: askingReviewer),
            ]),
            record(askingReviewer, "Review: Gate graph instead of a fixed guard", status: "waiting", group: "needsYou", ago: 120, [
                "needsYou": .bool(true), "needsYouSince": stamp(120), "pending": .array([.string(permission)]), "notify": .string("needs-input"), "notifyEffective": .string("needs-input"),
                "pendingSummary": .object(["kind": .string("permission"), "text": .string(command), "tool": .string("execute"), "at": stamp(120)]),
                "rowUnder": .string(asking), "gateFor": reviewer(asking), "gateRound": round(1, "reviewing"),
            ]),
            record(changes, "Keep-awake plugin (Amphetamine)", status: "running", group: "running", ago: 60, [
                "turnStartedAt": stamp(300), "landing": landing("changes", round: 1, words: "3 changes", tone: "plain", badge: "3 changes", reviewer: changesReviewer, findings: 3),
            ]),
            record(changesReviewer, "Review: Keep-awake plugin", status: "done", group: "done", ago: 2_400, [
                "hidden": .bool(true), "gateFor": reviewer(changes), "gateRound": round(1, "changes"),
            ]),
            record(reviewing, "Side threads: dig into limits", status: "idle", group: "running", ago: 180, [
                "childStatus": .string("running"), "landing": landing("reviewing", round: 2, words: "review 2/2…", tone: "accent", badge: "reviewing", reviewer: reviewingReviewer),
            ]),
            record(reviewingReviewer, "Review: Side threads", status: "running", group: "running", ago: 5, [
                "hidden": .bool(true), "turnStartedAt": stamp(180), "gateFor": reviewer(reviewing), "gateRound": round(2, "reviewing"),
            ]),
            record(closedReviewer, "Review: Side threads", status: "closed", group: "closed", ago: 7_200, [
                "hidden": .bool(true), "gateFor": reviewer(reviewing), "gateRound": round(0, "", label: "Land review"),
            ]),
            record(approved, "Fix the flaky switcher test", status: "done", group: "done", ago: 900, [
                "landing": landing("approved", round: 2, words: "approved", tone: "success", badge: "approved", reviewer: ""),
            ]),
        ]
    }

    static func transcripts(entry: (String, String, String) -> JSONValue) -> [String: [String: JSONValue]] {
        [
            askingReviewer: ["entries": .array([
                entry("user", "review-request", "Review the landing of feat/gate-graph into main: the diff, the commits, and the goal \"Gate graph instead of a fixed guard\". End with agents_land_verdict."),
                entry("agent", "review-1", "The gate graph replaces the fixed guard in plugins/gates. Running its tests before the verdict."),
                .object(["kind": .string("permission"), "id": .string(permission), "turn": .number(1), "toolCall": .object(["kind": .string("execute"), "rawInput": .object(["command": .string(command), "description": .string("Run the gate tests")]), "title": .string(command), "toolCallId": .string("review-run")]),
                         "options": .array([.object(["optionId": .string("allow"), "name": .string("Allow"), "kind": .string("allow_once")]), .object(["optionId": .string("deny"), "name": .string("Reject"), "kind": .string("reject_once")])]),
                         "card": .object(["text": .string("Claude wants to run \(command)"), "title": .string("Run a command?"), "command": .string(command), "caption": .string("Claude says: Run the gate tests"),
                                          "options": .array([.object(["id": .string("allow"), "label": .string("Allow once"), "kind": .string("allow_once"), "shortcut": .string("opt+1")]), .object(["id": .string("deny"), "label": .string("Reject"), "kind": .string("reject_once"), "shortcut": .string("opt+3")])])])]),
            ])],
            changesReviewer: ["entries": .array([
                entry("user", "review-request", "Review the landing of feat/keep-awake into main."),
                entry("agent", "review-1", "Three confirmed bugs: a hold outlives its session, the battery state is read once, and the offline row never dims. Sent to «Keep-awake plugin (Amphetamine)»."),
            ])],
        ]
    }

    static func opens(_ params: JSONValue) -> JSONValue? {
        guard let action = params["action"]?.stringValue, ["gateReview", "gateLanding"].contains(action) else { return nil }
        let node = params["nodeId"]?.stringValue ?? ""
        let session = node.hasSuffix("/" + action) ? String(node.dropLast(action.count + 1)) : (params["value"]?.stringValue?.split(separator: "/").last).map(String.init) ?? ""
        return .object(["open": .object(["session": .string(session)])])
    }
}
#endif
