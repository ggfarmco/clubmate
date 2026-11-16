import WebKit

final class DocumentWKWebView: WKWebView {
    var passesVerticalScroll = false
    private var passing = false

    #if os(macOS)
    override func scrollWheel(with event: NSEvent) {
        guard passesVerticalScroll, !event.modifierFlags.contains(.command) else { return super.scrollWheel(with: event) }
        if event.scrollingDeltaX != 0 || event.scrollingDeltaY != 0 {
            passing = abs(event.scrollingDeltaY) >= abs(event.scrollingDeltaX)
        }
        guard passing, let next = nextResponder else { return super.scrollWheel(with: event) }
        next.scrollWheel(with: event)
    }
    #endif
}
