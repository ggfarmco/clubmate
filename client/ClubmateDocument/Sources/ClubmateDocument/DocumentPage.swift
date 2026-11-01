import Foundation

public struct DocumentContent: Equatable, Sendable {
    public let path: String
    public let html: String
    public let rev: Int

    public init(path: String, html: String, rev: Int) {
        self.path = path
        self.html = html
        self.rev = rev
    }
}

public enum DocumentSize: Equatable, Sendable {
    case fill
    case fit
}

enum DocumentPage {
    enum Update: Equatable {
        case load
        case replaceBody
        case none
    }

    static let web = Bundle.module.resourceURL!.appendingPathComponent("Web", isDirectory: true)

    static func update(old: DocumentContent?, new: DocumentContent) -> Update {
        guard let old, old.path == new.path else { return .load }
        return old.rev == new.rev ? .none : .replaceBody
    }

    static func html(_ content: DocumentContent, theme: DocumentTheme, size: DocumentSize) -> String {
        let template = (try? String(contentsOf: web.appendingPathComponent("document.html"), encoding: .utf8)) ?? ""
        let page = template
            .replacingOccurrences(of: "{{size}}", with: size == .fit ? "fit" : "fill")
            .replacingOccurrences(of: "{{theme}}", with: theme.css)
        guard let body = page.range(of: "{{body}}") else { return page }
        return page.replacingCharacters(in: body, with: content.html)
    }
}
