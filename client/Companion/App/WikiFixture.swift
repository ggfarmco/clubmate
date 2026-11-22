#if DEBUG
import Foundation

import CompanionKit

enum WikiFixture {
    struct Page {
        let id: String
        let title: String
        let body: String
        var foot = ""
        var file: String?

        var path: String { file ?? WikiFixture.root + id.dropFirst("shop/".count) }
    }

    static let sidebar = WikiPluginInfo.id + ".sidebar"
    static let main = WikiPluginInfo.id + ".main"
    static let chat = WikiPluginInfo.id + ".chat"
    static let root = "/Users/me/src/shop/wiki/"

    static var on: Bool {
        UserDefaults.standard.bool(forKey: "wiki")
    }

    static let section = JSONValue.object(["id": .string(WikiPluginInfo.id), "title": .string("Wiki"), "icon": .string("book"), "order": .number(40), "scope": .string("workspace"), "autoNew": .bool(false), "plugin": .string(WikiPluginInfo.id)])

    static let pages = [
        Page(id: "shop/README.md", title: "Shop wiki", body: "<p>Start with the <a href=\"clubmate-doc:\(root)checkout.md\">checkout flow</a> or the <a href=\"clubmate-doc:/Users/me/src/shop/docs/payments.md\">payments overview</a>.</p>", foot: "<p>Then <a href=\"clubmate-doc:\(root)checkout.md\">read the checkout flow next</a>.</p>"),
        Page(id: "shop/checkout.md", title: "Checkout flow", body: margin + "<p>Back to the <a href=\"clubmate-doc:\(root)README.md\">shop wiki</a>.</p><pre><code>booking.roots2:30:17: error: `Booking` needs `room`: only optional fields can be left out of create, and room is not optional here</code></pre>"),
        Page(id: "shop/notes/retries.md", title: "Retry notes", body: "<p>Charge retries keep the idempotency key.</p>"),
        Page(id: "shop/checkout-retries.md", title: "Checkout retries", body: "<p>Retries keep the idempotency key.</p>", file: "/Users/me/src/shop/docs/checkout-retries.md"),
    ] + guides

    static let margin = "<aside class=\"wiki-margin\" aria-label=\"About this page\"><nav class=\"wm-sec\"><div class=\"wm-cap\">On this page</div><a class=\"wm-oln\" href=\"#part-1\">Part 1</a></nav></aside><details class=\"wiki-fold\"><summary>On this page</summary><div class=\"wm-fold\"><a class=\"wm-oln\" href=\"#part-1\">Part 1</a></div></details>"

    static let guides = ["Payments", "Refunds", "Shipping rates", "Tax rules", "Inventory sync", "Search ranking", "Email templates", "Feature flags", "Release checklist", "On-call runbook", "Error budgets", "Data retention"].map { title in
        Page(id: "shop/guides/\(title.lowercased().replacing(" ", with: "-")).md", title: title, body: "<p>\(title) for the shop.</p>")
    }

    static func page(_ id: String?) -> Page? {
        pages.first { $0.id == id }
    }

    static func gone(_ id: String) -> Bool {
        UserDefaults.standard.string(forKey: "wikiGone") == id
    }

    static func locate(_ path: String, home: Bool = false) -> JSONValue {
        guard let page = home ? pages.first : pages.first(where: { $0.path == path }) else { return .object([:]) }
        return .object(["id": .string(page.id), "path": .string(page.path), "title": .string(page.title)])
    }

    static func sidebarTree(selected: String, query: String = "") -> JSONValue {
        func item(_ page: Page) -> JSONValue {
            .object(["id": .string(page.id), "label": .string(page.title), "icon": .string("doc.text")])
        }
        func folder(_ name: String, _ children: [Page]) -> JSONValue {
            .object(["id": .string("dir:shop/\(name)"), "label": .string(name), "hasChildren": .bool(true), "expanded": .bool(true), "action": .bool(true), "children": .array(children.map(item))])
        }
        let fresh = guides.prefix(2).map { page in
            JSONValue.object(["id": .string("new:" + page.id), "mirrorOf": .string(page.id), "label": .string(page.title), "icon": .string("circlebadge.fill"), "tone": .string("accent"), "detailTone": .string("neutral"), "detail": .string("agent · 08:41"), "truncate": .string("tail")])
        }
        let new = JSONValue.object(["id": .string("sec:new"), "label": .string("New to you"), "detail": .string("2"), "hasChildren": .bool(true), "expanded": .bool(true), "action": .bool(true), "children": .array(fresh)])
        let needle = query.trimmingCharacters(in: .whitespaces).lowercased()
        let items = needle.isEmpty ? [new, item(pages[0]), item(pages[1]), folder("notes", [pages[2]]), folder("guides", guides)] : pages.filter { $0.title.lowercased().contains(needle) }.map(item)
        let tree = JSONValue.object(["type": .string("tree"), "id": .string("pages"), "props": .object(["selected": .string(selected), "items": .array(items)])])
        let head = JSONValue.object(["type": .string("stack"), "props": .object(["direction": .string("horizontal")]), "children": .array([
            .object(["type": .string("text"), "props": .object(["text": .string("Wiki"), "style": .string("strong")])]),
            .object(["type": .string("text"), "props": .object(["text": .string("\(pages.count) pages"), "style": .string("muted")])]),
            .object(["type": .string("spacer")]),
            .object(["type": .string("button"), "id": .string("new"), "props": .object(["label": .string("New page"), "icon": .string("plus"), "variant": .string("ghost"), "iconOnly": .bool(true), "action": .string("new")])]),
        ])])
        let search = JSONValue.object(["type": .string("textField"), "id": .string("search"), "props": .object(["placeholder": .string("Search wiki"), "value": .string(query), "live": .bool(true), "action": .string("search")])])
        let body = JSONValue.object(["type": .string("stack"), "props": .object(["direction": .string("vertical")]), "children": .array([tree])])
        return .object(["type": .string("stack"), "props": .object(["direction": .string("vertical")]), "children": .array([head, search, .object(["type": .string("scroll"), "children": .array([body])])])])
    }

    static func mainTree(_ page: Page) -> JSONValue {
        let filler = (1...40).map { "<h2 id=\"part-\($0)\">Part \($0)</h2><p>\(page.title) part \($0) explains one step of the shop in detail.</p>" }.joined()
        let html = "<h1 id=\"top\">\(page.title)</h1>" + page.body + filler + page.foot + "<p>End of \(page.title).</p>"
        func button(_ id: String, _ label: String, _ icon: String, _ extra: [String: JSONValue]) -> JSONValue {
            .object(["type": .string("button"), "id": .string(id), "props": .object(["label": .string(label), "icon": .string(icon)].merging(extra) { $1 })])
        }
        let actions = JSONValue.array([
            button("edit", "Edit", "pencil", ["variant": .string("primary"), "action": .string("edit")]),
            button("history", "History", "clock.arrow.circlepath", ["variant": .string("secondary"), "action": .string("history")]),
            button("copy", "Copy path", "doc.on.doc", ["overflow": .bool(true), "copy": .string(page.path)]),
        ])
        let header = JSONValue.object(["type": .string("header"), "id": .string("header"), "props": .object(["title": .string(page.title), "icon": .string("doc.text"), "meta": .string("Updated 08:41 by agent")]), "children": actions])
        let document = JSONValue.object(["type": .string("document"), "id": .string("page"), "props": .object(["html": .string(html), "path": .string(page.path), "rev": .number(Double((pages.firstIndex { $0.id == page.id } ?? 0) + 1)), "size": .string("fill")])])
        return .object(["type": .string("stack"), "props": .object(["direction": .string("vertical")]), "children": .array([header, document])])
    }

    private static func node(_ type: String, _ id: String? = nil, _ props: [String: JSONValue] = [:], _ children: [JSONValue] = []) -> JSONValue {
        var fields: [String: JSONValue] = ["type": .string(type)]
        if let id { fields["id"] = .string(id) }
        if !props.isEmpty { fields["props"] = .object(props) }
        if !children.isEmpty { fields["children"] = .array(children) }
        return .object(fields)
    }

    static var chatPicker: JSONValue {
        func row(_ id: String, _ label: String, _ caption: String, badge: String? = nil) -> JSONValue {
            var fields: [String: JSONValue] = ["id": .string(id), "label": .string(label), "caption": .string(caption), "icon": .string("circle.fill")]
            if let badge { fields["badge"] = .string(badge); fields["tone"] = .string("warning") }
            return .object(fields)
        }
        let items = JSONValue.array([
            .object(["id": .string("suggested"), "label": .string("Suggested"), "section": .bool(true)]),
            row("agt_chat_1", "Rate-limit notifications", "last used here · 1h"),
            .object(["id": .string("open-in"), "label": .string("Open in shop"), "detail": .string("3"), "section": .bool(true)]),
            row("agt_chat_2", "Wiki: document review flow", "docs/wiki · 12m", badge: "needs you"),
            row("agt_chat_3", "Fix flaky land gate test", "main · 25m"),
            row("agt_chat_4", "Port cron to the detail pattern", "docs/design-brief · 2h"),
        ])
        let list = node("list", "chat/sessions", ["action": .string("chatPick"), "focus": .string("agt_chat_1"), "items": items, "filter": .object(["placeholder": .string("Filter sessions"), "minRows": .number(10)])])
        let add = node("button", "chat/new", ["label": .string("New session"), "icon": .string("plus"), "variant": .string("secondary"), "action": .string("chatNew"), "hint": .string("n")])
        let note = node("text", nil, ["text": .string("Sees the page Checkout flow"), "style": .string("muted")])
        let foot = node("stack", nil, ["direction": .string("vertical"), "gap": .number(6), "inset": .string("foot")], [add, note])
        let header = node("stack", nil, ["direction": .string("horizontal"), "gap": .number(8), "inset": .string("bar"), "notes": .bool(true)], [
            node("text", nil, ["text": .string("Chat"), "style": .string("strong")]),
            node("text", nil, ["text": .string("· pick a session"), "style": .string("muted"), "lines": .number(1)]),
            node("spacer"),
            hide,
        ])
        return node("keys", "chat/picker", ["keys": .array([.string("n")]), "action": .string("chatNew"), "hints": .string("↩ open · ↑↓ j k move · N new · / filter")], [node("stack", nil, ["direction": .string("vertical")], [header, list, node("divider"), foot])])
    }

    private static var hide: JSONValue {
        node("button", "chat/hide", ["label": .string("Hide chat"), "icon": .string("sidebar.right"), "variant": .string("ghost"), "iconOnly": .bool(true), "hint": .string("cmd+b"), "clientAction": .string("hideChat")])
    }

    static func chatSession(chip shown: Bool = true) -> JSONValue {
        let back = node("button", "chat/back", ["label": .string("All sessions"), "icon": .string("chevron.left"), "variant": .string("ghost"), "action": .string("chatBack"), "shortcut": .string("cmd+["), "iconOnly": .bool(true), "keyCap": .bool(true)])
        let title = node("text", nil, ["text": .string("Rate-limit notifications"), "style": .string("strong"), "lines": .number(1)])
        let head = node("stack", nil, ["direction": .string("horizontal"), "gap": .number(8), "inset": .string("bar")], [back, node("divider"), node("icon", nil, ["name": .string("circle.fill")]), title, node("spacer"), hide])
        let message = node("message", "agt_chat_1/msg/agent/1", ["role": .string("agent"), "text": .string("I can add a paragraph under Send about a closed opener.")])
        let transcript = node("transcript", "agt_chat_1", [:], [message])
        let chip = JSONValue.object(["id": .string("ctx/page"), "kind": .string("page"), "path": .string("Checkout flow page"), "context": .bool(true), "removable": .bool(true)])
        let composer = node("composer", "agt_chat_1/composer", ["attachments": .array(shown ? [chip] : []), "context": .string("Context")])
        return node("stack", nil, ["direction": .string("vertical")], [head, transcript, composer])
    }
}
#endif
