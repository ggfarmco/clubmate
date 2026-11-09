#if DEBUG
import SwiftUI

import CompanionKit

struct SDUIGalleryScreen: View {
    let focus: String
    @State private var last = "No event yet"
    @State private var position = ScrollPosition(y: CGFloat(UserDefaults.standard.double(forKey: "sdui-offset")))

    private static let tree = load()
    private static let extras = (try? JSONDecoder().decode(SDUINode.self, from: Data(extrasJSON.utf8))) ?? SDUINode(type: "text", props: ["text": .string("extras broken")])

    var body: some View {
        NavigationStack {
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                .background(Color(.systemGroupedBackground))
                .navigationTitle("SDUI Gallery")
                .navigationBarTitleDisplayMode(.inline)
                .safeAreaInset(edge: .bottom) {
                    Text(last)
                        .font(.caption.monospaced())
                        .lineLimit(2)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .glassEffect(.regular, in: .rect(cornerRadius: 16))
                        .padding(.horizontal, 12)
                        .accessibilityIdentifier("sdui.lastEvent")
                }
        }
    }

    @ViewBuilder
    private var content: some View {
        if focus == "all" || focus == "1" {
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    SDUINodeView(node: Self.tree, context: context("gallery.main"))
                    SDUINodeView(node: Self.extras, context: context("gallery.main"))
                }
                .padding(.vertical, 12)
            }
            .scrollPosition($position)
            .environment(\.sduiInScroll, true)
        } else if let node = Self.find(focus, in: Self.tree) ?? Self.find(focus, in: Self.extras) {
            SDUINodeView(node: node, context: context("gallery.sidebar"))
        } else {
            ContentUnavailableView("No node \(focus)", systemImage: "questionmark.square.dashed")
        }
    }

    private func context(_ slot: String) -> SDUIContext {
        SDUIContext(slot: slot, send: { id, action, value in
            let json = (try? JSONEncoder().encode(value ?? .null)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
            last = "\(id) · \(action) · \(json)"
        }, open: { slot in
            last = "open \(slot)"
        })
    }

    private static func find(_ id: String, in node: SDUINode) -> SDUINode? {
        if node.id == id { return node }
        for child in node.children {
            if let found = find(id, in: child) { return found }
        }
        return nil
    }

    private static func load() -> SDUINode {
        guard let url = Bundle.main.url(forResource: "GalleryFixture", withExtension: "json"), let data = try? Data(contentsOf: url), let node = try? JSONDecoder().decode(SDUINode.self, from: data) else {
            return SDUINode(type: "empty", props: ["text": .string("gallery fixture missing")])
        }
        return node
    }

    private static let extrasJSON = """
    {"type": "stack", "id": "phone-extras", "props": {"direction": "vertical"}, "children": [
      {"type": "text", "props": {"text": "PHONE EXTRAS", "style": "muted"}},
      {"type": "list", "id": "jobs", "props": {"selected": "j1", "items": [
        {"id": "j1", "label": "Nightly dependency audit for the whole monorepo", "detail": "every day at 03:00", "icon": "clock", "menu": [{"action": "run", "label": "Run now"}, {"action": "delete", "label": "Delete", "confirm": "Delete this job?"}]},
        {"id": "j2", "label": "Weekly digest", "detail": "Missed", "icon": "exclamationmark.triangle", "tone": "warning", "badge": "1"},
        {"id": "j3", "label": "Prune worktrees", "detail": "paused", "icon": "pause.circle"}]}},
      {"type": "stack", "props": {"direction": "horizontal"}, "children": [
        {"type": "toggle", "id": "paused", "props": {"value": false, "action": "pause"}},
        {"type": "text", "props": {"text": "Paused"}}]},
      {"type": "select", "id": "missed", "props": {"value": "run", "action": "missed", "options": [{"value": "run", "label": "Run once"}, {"value": "skip", "label": "Skip"}]}},
      {"type": "folderField", "id": "roots", "props": {"label": "Roots", "values": ["~/develop/clubmate", "~/develop/norimori/ggfarmco/clubmate-notes/superpowers"]}},
      {"type": "stack", "id": "rules", "props": {"direction": "vertical"}, "children": [
        {"type": "divider"},
        {"type": "text", "props": {"lines": 1, "text": "lines 1: Workspace is set only by the Always allow path. Say so in a comment, or the next reader will set it for Allow once too."}},
        {"type": "stack", "props": {"direction": "horizontal"}, "children": [
          {"type": "text", "props": {"text": "node.go:23", "style": "muted"}},
          {"type": "text", "props": {"lines": 1, "text": "Workspace is set only by the Always allow path. Say so in a comment."}},
          {"type": "spacer"},
          {"type": "text", "props": {"text": "open", "style": "muted"}}]},
        {"type": "divider"}]},
      {"type": "text", "props": {"lines": 2, "text": "Review the open pull requests in this repository, summarise what each one changes, flag anything that touches the release scripts or the signing setup, and post the summary to the team channel before 10:00 so the morning stand-up can go through it."}},
      {"type": "diff", "props": {"text": "--- a/cart.go\\n+++ b/cart.go\\n@@ -1,6 +1,6 @@\\n package cart\\n \\n-func Total(items []Item) int {\\n-\\ttotal := 0; for _, it := range items { total += it.Price }\\n+func Total(items []Item) int {\\n+\\ttotal := cartTotal(items)\\n \\treturn total\\n }\\n"}},
      {"type": "plan", "props": {"entries": [{"content": "Extract cartTotal", "status": "completed"}, {"content": "Add a test", "status": "in_progress"}, {"content": "Run the tests", "status": "pending"}]}},
      {"type": "toolCard", "id": "tc-edit", "props": {"kind": "edit", "title": "Edit cart.go", "status": "completed", "body": {"format": "diff", "text": "--- a/cart.go\\n+++ b/cart.go\\n@@ -2,1 +2,1 @@\\n-total := 0\\n+total := cartTotal(items)\\n"}}},
      {"type": "toolCard", "id": "tc-failed", "props": {"kind": "execute", "title": "go vet ./...", "status": "failed", "body": {"format": "terminal", "text": "exit status 1"}}},
      {"type": "futureWidget", "id": "future"}
    ]}
    """
}
#endif
