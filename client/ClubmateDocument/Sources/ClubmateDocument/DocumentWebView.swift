import SwiftUI
import WebKit

public struct DocumentScroll: Equatable, Sendable {
    public let offset: Double
    public let delta: Double
    public let progress: Double
    public let tracking: Bool
}

public struct DocumentWebView: View {
    private let content: DocumentContent
    private let theme: DocumentTheme
    private let size: DocumentSize
    private let zoom: DocumentZoom?
    private let find: DocumentFind?
    private let header: AnyView?
    private let onScroll: ((DocumentScroll) -> Void)?
    private let onTextTap: (() -> Void)?
    private let hardTopEdge: Bool
    private let onLink: (DocumentLink) -> Void
    @State private var height: CGFloat = 0

    public init(content: DocumentContent, theme: DocumentTheme, size: DocumentSize, zoom: DocumentZoom? = nil, find: DocumentFind? = nil, header: AnyView? = nil, onScroll: ((DocumentScroll) -> Void)? = nil, onTextTap: (() -> Void)? = nil, hardTopEdge: Bool = false, onLink: @escaping (DocumentLink) -> Void) {
        self.content = content
        self.theme = theme
        self.size = size
        self.zoom = zoom
        self.find = find
        self.header = header
        self.onScroll = onScroll
        self.onTextTap = onTextTap
        self.hardTopEdge = hardTopEdge
        self.onLink = onLink
    }

    public var body: some View {
        let size = content.kind == .artifact ? .fill : size
        DocumentRepresentable(content: content, theme: theme, size: size, zoom: zoom, find: find, scale: zoom?.scale ?? 1, header: header, onScroll: onScroll, onTextTap: onTextTap, hardTopEdge: hardTopEdge, onLink: onLink) { height = $0 }
            .id(content.kind)
            .frame(height: size == .fit ? height : nil)
    }
}

@MainActor
private struct DocumentRepresentable {
    let content: DocumentContent
    let theme: DocumentTheme
    let size: DocumentSize
    let zoom: DocumentZoom?
    let find: DocumentFind?
    let scale: Double
    let header: AnyView?
    let onScroll: ((DocumentScroll) -> Void)?
    let onTextTap: (() -> Void)?
    let hardTopEdge: Bool
    let onLink: (DocumentLink) -> Void
    let onHeight: (CGFloat) -> Void

    func makeCoordinator() -> DocumentController {
        DocumentController(kind: content.kind)
    }

    private func update(_ controller: DocumentController) {
        controller.onLink = onLink
        controller.onHeight = onHeight
        controller.onScroll = onScroll
        controller.onTextTap = onTextTap
        #if os(iOS)
        controller.setHeader(header)
        controller.setHardTopEdge(hardTopEdge)
        #endif
        if controller.zoom !== zoom { controller.zoom = zoom }
        if controller.finder !== find { controller.finder = find }
        controller.show(content, theme: theme, size: size)
        controller.setScale(scale)
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
