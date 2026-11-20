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

    #if os(iOS)
    var header: UIView? {
        didSet {
            guard header !== oldValue else { return }
            oldValue?.removeFromSuperview()
            if let header { scrollView.addSubview(header) }
            setNeedsLayout()
        }
    }
    private var headerHeight: CGFloat = 0

    override func layoutSubviews() {
        super.layoutSubviews()
        let height = header.map { ceil($0.sizeThatFits(CGSize(width: bounds.width, height: .greatestFiniteMagnitude)).height) } ?? 0
        header?.frame = CGRect(x: 0, y: -height, width: bounds.width, height: height)
        guard height != headerHeight else { return }
        let atTop = scrollView.contentOffset.y <= -scrollView.adjustedContentInset.top + 1
        scrollView.contentInset.top += height - headerHeight
        headerHeight = height
        if atTop { scrollView.contentOffset.y = -scrollView.adjustedContentInset.top }
    }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        guard window != nil, scrollView.isScrollEnabled else { return }
        var responder = next
        while let current = responder, !(current is UIViewController) { responder = current.next }
        (responder as? UIViewController)?.setContentScrollView(scrollView)
    }
    #endif
}
