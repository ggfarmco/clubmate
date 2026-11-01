import Foundation

public enum DocumentLink: Equatable, Sendable {
    case doc(path: String, anchor: String?)
    case file(path: String)
    case external(URL)
    case anchor(String)
    case ignore

    public init(_ url: URL) {
        switch url.scheme?.lowercased() {
        case "clubmate-doc":
            self = url.path.isEmpty ? .ignore : .doc(path: url.path, anchor: url.fragment)
        case "clubmate-file":
            self = url.path.isEmpty ? .ignore : .file(path: url.path)
        case "http", "https":
            self = url.host() == nil ? .ignore : .external(url)
        case nil:
            if let fragment = url.fragment, url.path.isEmpty, !fragment.isEmpty {
                self = .anchor(fragment)
            } else {
                self = .ignore
            }
        default:
            self = .ignore
        }
    }
}
