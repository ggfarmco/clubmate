import Foundation
import WebKit

@MainActor
public final class WebPageFinder: FindTarget {
    private weak var webView: WKWebView?
    private weak var finder: DocumentFind?
    private var loading: NSKeyValueObservation?

    public init(webView: WKWebView, find: DocumentFind) {
        self.webView = webView
        finder = find
        find.target = self
        loading = webView.observe(\.isLoading) { [weak self] webView, _ in
            MainActor.assumeIsolated {
                guard !webView.isLoading, let finder = self?.finder else { return }
                Task { await finder.refresh() }
            }
        }
    }

    func find(_ command: FindCommand) async -> FindResult? {
        guard let webView else { return nil }
        let (script, arguments) = command.script
        let source = "if (!window.clubmateFind) {\nwindow.clubmateFindFixed = true;\n\(DocumentController.findScript)\n}\n\(script)"
        return FindResult(try? await webView.callAsyncJavaScript(source, arguments: arguments, contentWorld: .defaultClient))
    }
}
