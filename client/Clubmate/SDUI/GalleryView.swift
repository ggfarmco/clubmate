import Foundation
import SwiftUI

import ClubmateKit

struct GalleryView: View {
    private let tree = GalleryView.loadFixture()

    @Environment(\.theme) private var theme

    var body: some View {
        SDUIView(node: tree, slot: "gallery.main", projectID: "prj_0000000000000000", send: GalleryView.echo)
            .frame(minWidth: 1000, minHeight: 760)
            .background(theme.background)
    }

    private static let echo: UIEventSink = { event in
        guard event.action != "fail" else {
            return .failure(.rpc(code: -32013, message: "gallery: simulated plugin error"))
        }
        return .success(.object(["action": .string(event.action), "value": event.value]))
    }

    private static func loadFixture() -> SDUINode {
        guard let url = Bundle.main.url(forResource: "GalleryFixture", withExtension: "json"),
              let data = try? Data(contentsOf: url),
              let node = try? JSONDecoder().decode(SDUINode.self, from: data)
        else {
            return SDUINode(type: "empty", props: ["text": .string("gallery fixture missing")])
        }
        return node
    }
}
