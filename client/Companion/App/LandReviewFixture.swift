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
    static let held = "agt_00000000000000b9"
    static let heldCard = "agt_00000000000000ba"
    static let heldRequest = "lg_00000000000000ba"
    static let heldCall = "agt_00000000000000bb"
    static let twoGates = "agt_00000000000000bc"
    static let refusedCall = "agt_00000000000000bd"
    static let callReviewer = "agt_00000000000000be"
    static let permission = "permission-r1"
    static let command = "go test ./plugins/gates/..."

    static var on: Bool {
        UserDefaults.standard.bool(forKey: "landReview")
    }

    static let section = JSONValue.object(["id": .string("agents.gates"), "title": .string("Gates"), "icon": .string("checkmark.shield"), "order": .number(38), "scope": .string("workspace"), "autoNew": .bool(false), "plugin": .string("agents"), "rail": .bool(false)])

    static var dock: [JSONValue] { [
        .object(["id": .string("service"), "label": .string("Service"), "icon": .string("gearshape.2"), "detail": .string("0"), "phoneDetail": .string("0")]),
        GateTrustFixture.on
            ? .object(["id": .string("gates"), "label": .string("Gates"), "icon": .string("checkmark.shield"), "detail": .string("2"), "phoneDetail": .string("2"), "state": .string("1 to trust"), "which": .string("Done · plans and specs"), "attention": .bool(true)])
            : .object(["id": .string("gates"), "label": .string("Gates"), "icon": .string("checkmark.shield"), "detail": .string("1"), "phoneDetail": .string("1"), "state": .string("1 asking"), "which": .string("into main")]),
        .object(["id": .string("graphs"), "label": .string("Graphs"), "icon": .string("point.3.connected.trianglepath.dotted"), "detail": .string("2"), "phoneDetail": .string("2"), "state": .string("1 to trust"), "which": .string("Plan pipeline"), "attention": .bool(true)]),
    ] }

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
        func callLanding(line: String, badge: String) -> JSONValue {
            .object(["round": .number(1), "of": .number(2), "verdict": .string("reviewing"), "words": .string("held"), "tone": .string("accent"), "badge": .string(badge), "badgeTone": .string("accent"), "into": .string(""), "held": .string("git push"), "line": .string(line), "hint": .string("Held by a gate — messages queue")])
        }
        let branches = [heldCall: "fix-cart-total", twoGates: "fix-cart-total", refusedCall: "fix-cart-total", held: "main", heldCard: "main", asking: "gate-graph", askingReviewer: "gate-graph", changes: "keep-awake", changesReviewer: "keep-awake", reviewing: "side-threads", reviewingReviewer: "side-threads", closedReviewer: "side-threads", approved: "flaky-switcher"]
        let reviewer = { (lander: String) in JSONValue.object(["lander": .string(lander), "into": .string("main")]) }
        let rows = [
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
            record(held, "Write the inbox plan", status: "running", group: "running", ago: 40, [
                "turnStartedAt": stamp(400), "turnHeld": .bool(true), "heldRound": .object(["text": .string("Plan pipeline · round 1 changes · round 2 reviewing"), "run": .string("gr_00000000000000b9")]),
                "landing": .object(["round": .number(1), "of": .number(1), "verdict": .string("reviewing"), "words": .string("held"), "tone": .string("accent"), "badge": .string("held · plans and specs"), "badgeTone": .string("accent"), "into": .string(""), "held": .string("plans and specs"), "line": .string("Held · plans and specs · Plan pipeline…")]),
            ]),
            record(heldCall, "Ship the cart fix", status: "running", group: "running", ago: 40, [
                "turnStartedAt": stamp(300), "landing": callLanding(line: "Held · git push · Fable review…", badge: "held · git push"),
            ]),
            record(twoGates, "Open the cart PR", status: "running", group: "running", ago: 50, [
                "turnStartedAt": stamp(320), "landing": callLanding(line: "Held · git push +1 · 2 graphs reviewing", badge: "held · git push +1"),
            ]),
            record(refusedCall, "Fix the discount order", status: "running", group: "running", ago: 20, [
                "turnStartedAt": stamp(200),
            ]),
            record(callReviewer, "Review: git push", status: "running", group: "running", ago: 5, [
                "hidden": .bool(true), "turnStartedAt": stamp(40),
            ]),
            record(heldCard, "Plan rate limits", status: "running", group: "needsYou", ago: 90, [
                "turnHeld": .bool(true), "heldRound": .object(["text": .string("Plan pipeline · round 1 changes · round 2 changes"), "run": .string("gr_00000000000000ba")]), "needsYou": .bool(true), "needsYouSince": stamp(90), "pending": .array([.string(heldRequest)]), "requests": .array([.string(heldRequest)]),
                "pendingSummary": .object(["kind": .string("land"), "text": .string("Plan pipeline still asks for changes after 2 rounds"), "onMac": .bool(true), "at": stamp(90)]),
            ]),
        ]
        guard GateTrustFixture.on else { return rows }
        return rows + [record(GateTrustFixture.gate, "Done · plans and specs", status: "waiting", group: "needsYou", ago: 240, [
            "kind": .string("gate"), "rowDetail": .string("Graph changed · 1 waits"), "needsYou": .bool(true), "needsYouSince": stamp(240), "listFilters": .array([.string("needs")]),
        ])]
    }

    static func transcripts(entry: (String, String, String) -> JSONValue) -> [String: [String: JSONValue]] {
        let option = { (id: String, label: String, kind: String?, key: String) in
            JSONValue.object(["id": .string(id), "label": .string(label), "shortcut": .string(key)].merging(kind.map { ["kind": .string($0)] } ?? [:]) { $1 })
        }
        let heldNode = JSONValue.object(["type": .string("request.land"), "id": .string(heldCard + "/landgate/" + heldRequest), "props": .object([
            "request": .string(heldRequest), "title": .string("Plan pipeline still asks for changes after 2 rounds"), "name": .string("Plan rate limits"), "dock": .bool(true),
            "caption": .string("shop · Done · plans and specs · held since 12:41"), "gate": .bool(true), "question": .string("Accept the plan as done?"),
            "detail": .string("- Fable review — the plan still doesn't say who owns the muted-workspace setting\n- Lint — plans/inbox-groups.md:12 line length\n\nSummary: Groups the inbox by workspace, then by day; muted workspaces fold into one row."),
            "rule": .string("Accept as done shows the turn as done as it is. One more round sends the agent these findings and reviews its next finished turn. Stop leaves the session idle and its turn not done."),
            "options": .array([option("merge", "Accept as done", "allow_once", "opt+1"), option("more", "One more round", nil, "opt+2"), option("leave", "Stop", "reject_once", "opt+3")]),
        ])])
        return [
            heldCall: ["entries": .array([
                entry("user", "call-1", "Fix the cart total and push the branch"),
                entry("agent", "call-2", "The cart total keeps the stored total when the items slice is nil, and discounts are subtracted again. go test ./internal/cart/... passes. Pushing the branch so the PR can open."),
                json(#"{"kind":"tool","id":"call-edit","turn":1,"tool":{"kind":"edit","title":"Edit internal/cart/total.go","status":"completed","toolCallId":"call-edit"}}"#),
                json(#"{"kind":"tool","id":"call-test","turn":1,"tool":{"kind":"execute","title":"go test ./internal/cart/...","status":"completed","rawInput":{"command":"go test ./internal/cart/..."},"toolCallId":"call-test"}}"#),
                json(#"{"kind":"tool","id":"call-push","turn":1,"tool":{"kind":"execute","title":"git push origin fix/cart-total","status":"pending","rawInput":{"command":"git push origin fix/cart-total"},"toolCallId":"call-push"},"gate":{"state":"held","title":"Held by a gate","caption":"shop · git push · round 1 of 2","tool":"Bash","input":"git push origin fix/cart-total","lines":[{"text":"Fable review → approved / changes","state":"reviewing · <1 min","round":1,"review":"\#(callReviewer)"}],"note":"Claude waits inside this call. Approved runs it. Changes refuse it, and the gate then sends its findings in a message."}}"#),
            ])],
            twoGates: ["entries": .array([
                entry("user", "two-1", "Push the branch and open the PR"),
                json(#"{"kind":"tool","id":"two-push","turn":1,"tool":{"kind":"execute","title":"git push origin fix/cart-total && gh pr create --fill","status":"pending","rawInput":{"command":"git push origin fix/cart-total && gh pr create --fill"},"toolCallId":"two-push"},"gate":{"state":"held","title":"Held by 2 gates","caption":"shop · runs when both approve","tool":"Bash","input":"git push origin fix/cart-total && gh pr create --fill","lines":[{"text":"git push · Fable review · round 1 of 2","state":"approved · waits for gh pr create","round":1,"review":"\#(callReviewer)"},{"text":"gh pr create · PR review · round 1 of 1","state":"reviewing · <1 min","round":1,"review":"\#(callReviewer)"}],"note":"Claude waits inside this call. If either gate asks for changes, the refusal waits for the other; then the findings of both follow it in one message."}}"#),
            ])],
            refusedCall: ["entries": .array([
                entry("user", "ref-1", "Fix the discount order and push"),
                entry("agent", "ref-2", "The cart total keeps the stored total when the items slice is nil. Tests pass. Pushing the branch so the PR can open."),
                json(#"{"kind":"tool","id":"ref-push","turn":1,"tool":{"kind":"execute","title":"git push origin fix/cart-total","status":"failed","rawInput":{"command":"git push origin fix/cart-total"},"toolCallId":"ref-push"},"gate":{"state":"refused","title":"Refused by the gate · round 1 · changes","caption":"shop · git push · round 1 of 2","tool":"Bash","input":"git push origin fix/cart-total"}}"#),
                json(#"{"kind":"agent","id":"ref-3","turn":2,"text":"The gate found two things. Moving the discount after the nil check and adding the empty-cart test, then pushing again."}"#),
                json(#"{"kind":"user","id":"gate-ref-push","turn":2,"text":"[Clubmate] From the gate · shop · git push · round 1 of 2 · changes","gateMessage":{"groups":[{"head":"[Clubmate] From the gate · shop · git push · round 1 of 2 · changes","title":"From the gate · shop · git push · round 1 of 2 · changes","findings":["internal/cart/total.go:5 — discounts are applied before the nil check, so an empty cart can go negative","internal/cart/total_test.go — no test covers a discount on an empty cart"],"deferred":["total.go — the helper name reads like a getter"]}],"tail":"The next git push goes through the gate again, as round 2.","call":"ref-push"}}"#),
                json(#"{"kind":"tool","id":"ref-edit","turn":2,"tool":{"kind":"edit","title":"Edit internal/cart/total.go","status":"completed","toolCallId":"ref-edit"}}"#),
                json(#"{"kind":"tool","id":"ref-late","turn":2,"tool":{"kind":"execute","title":"git push origin fix/cart-total","status":"failed","rawInput":{"command":"git push origin fix/cart-total"},"toolCallId":"ref-late"},"gate":{"state":"refused","word":"refused"}}"#),
            ])],
            held: ["entries": .array([
                entry("user", "held-1", "Write the inbox plan"),
                entry("agent", "held-2", "Updated the plan after the second review: the ordering is now workspace, then day, and muted workspaces fold. Ready for you to read."),
            ])],
            heldCard: ["request": heldNode, "entries": .array([
                entry("user", "card-1", "Plan rate limits"),
                entry("agent", "card-2", "Updated the plan after the second review: the ordering is now workspace, then day, and muted workspaces fold."),
            ])],
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

    private static func json(_ text: String) -> JSONValue {
        (try? JSONDecoder().decode(JSONValue.self, from: Data(text.utf8))) ?? .null
    }

    static func opens(_ params: JSONValue) -> JSONValue? {
        guard let action = params["action"]?.stringValue, ["gateReview", "gateLanding"].contains(action) else { return nil }
        let node = params["nodeId"]?.stringValue ?? ""
        let session = node.hasSuffix("/" + action) ? String(node.dropLast(action.count + 1)) : (params["value"]?.stringValue?.split(separator: "/").last).map(String.init) ?? ""
        return .object(["open": .object(["session": .string(session)])])
    }
}
enum GateTrustFixture {
    static let gate = "gt_00000000000000d1"

    static var on: Bool {
        UserDefaults.standard.bool(forKey: "gateTrust")
    }
}
#endif
