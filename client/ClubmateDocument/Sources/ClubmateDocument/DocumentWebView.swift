import SwiftUI
import WebKit

public struct DocumentWebView: View {
    private let content: DocumentContent
    private let theme: DocumentTheme
    private let size: DocumentSize
    private let onLink: (DocumentLink) -> Void
    @State private var height: CGFloat = 0

    public init(content: DocumentContent, theme: DocumentTheme, size: DocumentSize, onLink: @escaping (DocumentLink) -> Void) {
        self.content = content
        self.theme = theme
        self.size = size
        self.onLink = onLink
    }

    public var body: some View {
        DocumentRepresentable(content: content, theme: theme, size: size, onLink: onLink) { height = $0 }
            .frame(height: size == .fit ? height : nil)
    }
}

@MainActor
private struct DocumentRepresentable {
    let content: DocumentContent
    let theme: DocumentTheme
    let size: DocumentSize
    let onLink: (DocumentLink) -> Void
    let onHeight: (CGFloat) -> Void

    func makeCoordinator() -> DocumentController {
        DocumentController()
    }

    private func update(_ controller: DocumentController) {
        controller.onLink = onLink
        controller.onHeight = onHeight
        controller.show(content, theme: theme, size: size)
    }
}

#if os(macOS)
extension DocumentRepresentable: NSViewRepresentable {
    func makeNSView(context: Context) -> WKWebView {
        context.coordinator.webView
    }

    func updateNSView(_ view: WKWebView, context: Context) {
        update(context.coordinator)
    }

    static func dismantleNSView(_ view: WKWebView, coordinator: DocumentController) {
        coordinator.close()
    }
}
#else
extension DocumentRepresentable: UIViewRepresentable {
    func makeUIView(context: Context) -> WKWebView {
        context.coordinator.webView
    }

    func updateUIView(_ view: WKWebView, context: Context) {
        update(context.coordinator)
    }

    static func dismantleUIView(_ view: WKWebView, coordinator: DocumentController) {
        coordinator.close()
    }
}
#endif
