import Foundation

public struct DocumentContent: Equatable, Sendable {
    public let path: String
    public let html: String
    public let rev: Int
    public let anchor: String?
    public let kind: DocumentKind

    public init(path: String, html: String, rev: Int, anchor: String? = nil, kind: DocumentKind = .markdown) {
        self.path = path
        self.html = html
        self.rev = rev
        self.anchor = anchor
        self.kind = kind
    }
}

public enum DocumentKind: String, Equatable, Hashable, Sendable {
    case markdown
    case artifact
}

public enum DocumentSize: Equatable, Sendable {
    case fill
    case fit
}

enum DocumentPage {
    enum Update: Equatable {
        case load
        case replaceBody
        case scroll
        case none
    }

    static let web = Bundle.module.resourceURL!.appendingPathComponent("Web", isDirectory: true)

    static func update(old: DocumentContent?, new: DocumentContent) -> Update {
        guard let old, old.path == new.path, old.kind == new.kind else { return .load }
        if old.rev != new.rev { return new.kind == .artifact ? .load : .replaceBody }
        return new.anchor != nil && new.anchor != old.anchor ? .scroll : .none
    }

    static func html(_ content: DocumentContent, theme: DocumentTheme, size: DocumentSize) -> String {
        let template = (try? String(contentsOf: web.appendingPathComponent("document.html"), encoding: .utf8)) ?? ""
        let page = template
            .replacingOccurrences(of: "{{size}}", with: size == .fit ? "fit" : "fill")
            .replacingOccurrences(of: "{{scale}}", with: size == .fit ? "maximum-scale=1, user-scalable=no" : "minimum-scale=1, maximum-scale=4")
            .replacingOccurrences(of: "{{theme}}", with: theme.css)
        guard let body = page.range(of: "{{body}}") else { return page }
        return page.replacingCharacters(in: body, with: content.html)
    }
}
