import Foundation

public enum WebViewNavigation: Equatable, Sendable {
    case none
    case load(URL)
    case reload

    public static func decide(current: URL?, currentRev: Int, url: URL, rev: Int) -> WebViewNavigation {
        guard current == url else { return .load(url) }
        return currentRev == rev ? .none : .reload
    }
}
