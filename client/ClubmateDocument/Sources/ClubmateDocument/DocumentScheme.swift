import Foundation
import WebKit

@MainActor
final class DocumentScheme: NSObject, WKURLSchemeHandler {
    static let name = "clubmate-document"
    static let base = URL(string: "\(name)://bundle/")!

    private static let files = ["document.css": "text/css", "document.js": "text/javascript", "mermaid.min.js": "text/javascript"]

    func webView(_ webView: WKWebView, start task: any WKURLSchemeTask) {
        guard let url = task.request.url,
              url.host() == Self.base.host(),
              url.path == "/" + url.lastPathComponent,
              let type = Self.files[url.lastPathComponent],
              let data = try? Data(contentsOf: DocumentPage.web.appendingPathComponent(url.lastPathComponent))
        else {
            task.didFailWithError(URLError(.fileDoesNotExist))
            return
        }
        task.didReceive(URLResponse(url: url, mimeType: type, expectedContentLength: data.count, textEncodingName: "utf-8"))
        task.didReceive(data)
        task.didFinish()
    }

    func webView(_ webView: WKWebView, stop task: any WKURLSchemeTask) {}
}
