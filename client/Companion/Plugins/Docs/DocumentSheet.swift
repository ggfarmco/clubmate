import SwiftUI

import ClubmateDocument
import CompanionKit

struct DocumentSheet: View {
    let doc: OpenDoc
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        NavigationStack {
            if let connection = model.connection(doc.computer) {
                DocumentScreen(connection: connection, doc: doc)
                    .id(ObjectIdentifier(connection))
            } else {
                ContentUnavailableView("Mac Not Paired", systemImage: "desktopcomputer", description: Text("Pair this Mac again to read its documents."))
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("Done") { navigation.doc = nil }
                        }
                    }
            }
        }
    }
}

private struct DocumentScreen: View {
    let connection: ComputerConnection
    let doc: OpenDoc
    @State private var store: DocsStore
    @State private var observer: UUID?
    @Environment(AppNavigation.self) private var navigation
    @Environment(\.colorScheme) private var scheme
    @Environment(\.openURL) private var openURL

    init(connection: ComputerConnection, doc: OpenDoc) {
        self.connection = connection
        self.doc = doc
        _store = State(initialValue: DocsStore(connection: connection, workspace: doc.workspace))
    }

    var body: some View {
        content
            .navigationTitle(store.stack.last?.title ?? "")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if store.stack.count > 1 {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("Back", systemImage: "chevron.backward") { store.back() }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { navigation.doc = nil }
                }
            }
            .safeAreaInset(edge: .bottom) {
                if let error = store.error, !store.stack.isEmpty {
                    Label(error, systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(Color.dangerInk)
                        .lineLimit(2)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 10)
                        .glassEffect(.regular, in: .capsule)
                        .padding(.bottom, 8)
                }
            }
            .onAppear {
                guard observer == nil else { return }
                observer = connection.observe { [store] in store.handle($0) }
            }
            .onDisappear {
                if let observer { connection.removeObserver(observer) }
                observer = nil
            }
            .task {
                if store.stack.isEmpty { await store.open(doc.path) }
            }
    }

    @ViewBuilder
    private var content: some View {
        if let page = store.stack.last {
            DocumentWebView(content: DocumentContent(path: page.path, html: page.html, rev: page.rev), theme: .system(dark: scheme == .dark), size: .fill, onLink: follow)
                .ignoresSafeArea(.container, edges: .bottom)
        } else if let error = store.error {
            ContentUnavailableView("Can't Open Document", systemImage: "doc.questionmark", description: Text(error))
        } else {
            ProgressView()
        }
    }

    private func follow(_ link: DocumentLink) {
        switch link {
        case .doc(let path, _):
            Task { await store.open(path) }
        case .external(let url):
            openURL(url)
        case .file, .anchor, .ignore:
            break
        }
    }
}
