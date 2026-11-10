import Foundation
import WebKit

@MainActor
final class ArtifactScheme: NSObject, WKURLSchemeHandler {
    static let name = "clubmate-artifact"
    static let policy = "default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval' blob: https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh; style-src 'unsafe-inline' https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh; font-src data: https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh; img-src data: blob: https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'"

    private(set) var origin = ""
    private var path = ""
    private var html = Data()

    func serve(path: String, html: String) -> URL {
        origin = UUID().uuidString.lowercased()
        self.html = Data(html.utf8)
        var components = URLComponents()
        components.scheme = Self.name
        components.host = origin
        components.path = path.hasPrefix("/") ? path : "/" + path
        let url = components.url ?? URL(fileURLWithPath: "/")
        self.path = url.path
        return url
    }

    func owns(_ url: URL) -> Bool {
        url.scheme == Self.name && url.host() == origin
    }

    func isPage(_ url: URL) -> Bool {
        owns(url) && url.path == path
    }

    func webView(_ webView: WKWebView, start task: any WKURLSchemeTask) {
        guard let url = task.request.url else { return }
        let found = isPage(url)
        let headers = found ? ["Content-Type": "text/html; charset=utf-8", "Content-Security-Policy": Self.policy] : ["Content-Type": "text/plain"]
        task.didReceive(HTTPURLResponse(url: url, statusCode: found ? 200 : 404, httpVersion: "HTTP/1.1", headerFields: headers)!)
        task.didReceive(found ? html : Data())
        task.didFinish()
    }

    func webView(_ webView: WKWebView, stop task: any WKURLSchemeTask) {}
}
