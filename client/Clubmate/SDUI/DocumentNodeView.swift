import AppKit
import SwiftUI

import ClubmateDocument
import ClubmateKit

struct DocumentNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme
    @Environment(\.sduiDispatch) private var dispatch

    private var content: DocumentContent {
        DocumentContent(path: node.string("path") ?? "", html: node.string("html") ?? "", rev: Int(node.props["rev"]?.numberValue ?? 0), anchor: node.string("anchor"))
    }

    var body: some View {
        DocumentWebView(content: content, theme: theme.document, size: node.string("size") == "fit" ? .fit : .fill) { link in
            open(link)
        }
        .frame(maxWidth: .infinity, maxHeight: node.string("size") == "fit" ? nil : .infinity)
    }

    private func open(_ link: DocumentLink) {
        switch link {
        case .doc(let path, let anchor):
            dispatch(node, .named("openDoc", .string(anchor.map { "\(path)#\($0)" } ?? path)))
        case .file(let path):
            dispatch(node, .named("openFile", .string(path)))
        case .external(let url):
            NSWorkspace.shared.open(url)
        case .anchor, .ignore:
            break
        }
    }
}
