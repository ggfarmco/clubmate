import Foundation
import Observation
import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class OpenProjectModel {
    let projectID: String
    let deck = SurfaceDeck()
    private(set) var store: ProjectUIStore?
    private(set) var failure: String?

    @ObservationIgnored private var client: CoreClient?

    init(projectID: String) {
        self.projectID = projectID
    }

    static func launchProjectID(_ arguments: [String] = CommandLine.arguments) -> String? {
        guard let index = arguments.firstIndex(of: "--open-project"), arguments.indices.contains(index + 1) else { return nil }
        return arguments[index + 1]
    }

    func start() async {
        guard client == nil, let cli = Bundle.main.resourceURL?.appending(path: "bin/clubmate") else { return }
        do {
            let paths = ClubmatePaths.resolve()
            try await CoreLauncher(cliPath: cli).ensureRunning(paths: paths)
            let client = try await CoreClient.connect(paths: paths)
            self.client = client
            let store = ProjectUIStore(client: client, projectID: projectID)
            try await store.start()
            self.store = store
        } catch {
            failure = "\(error)"
        }
    }

    func stop() async {
        deck.closeAll()
        await store?.stop()
        await client?.close()
        store = nil
        client = nil
    }
}

struct OpenProjectView: View {
    @State var model: OpenProjectModel

    @Environment(\.theme) private var theme

    var body: some View {
        content
            .frame(minWidth: 800, minHeight: 500)
            .background(theme.background, ignoresSafeAreaEdges: .all)
            .environment(\.surfaceDeck, model.deck)
            .task { await model.start() }
            .onDisappear { Task { await model.stop() } }
    }

    @ViewBuilder private var content: some View {
        if let store = model.store, let section = store.selectedSection {
            HStack(spacing: 0) {
                slot("\(section).sidebar", store).frame(width: 220)
                Rectangle().fill(theme.border).frame(width: 1)
                slot("\(section).main", store)
            }
            .onChange(of: store.slots, initial: true) { _, slots in
                model.deck.sync(referenced: SurfaceDeck.sessionIDs(in: slots))
            }
        } else {
            EmptyNodeView(text: model.failure ?? "Connecting…")
        }
    }

    @ViewBuilder private func slot(_ name: String, _ store: ProjectUIStore) -> some View {
        if let view = store.slots[name] {
            SDUIView(node: view.tree, slot: name, projectID: store.projectID) { [store] event in
                await store.send(event)
            }
        } else {
            EmptyNodeView(text: "")
        }
    }
}
