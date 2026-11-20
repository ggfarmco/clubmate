import SwiftUI

import ClubmateDocument
import CompanionKit

struct DocPageView: View {
    let connection: ComputerConnection
    let path: String
    let anchor: String?
    @State private var store: DocsStore
    @State private var observer: UUID?
    @State private var find = DocumentFind()
    @Environment(AppNavigation.self) private var navigation
    @Environment(\.colorScheme) private var scheme
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.openURL) private var openURL
    @Environment(\.documentReading) private var reading
    @Environment(\.backChipShown) private var chip
    @Environment(\.sectionLine) private var sectionLine

    init(connection: ComputerConnection, workspace: String, path: String, anchor: String?) {
        self.connection = connection
        self.path = path
        self.anchor = anchor
        _store = State(initialValue: DocsStore(connection: connection, workspace: workspace))
    }

    var body: some View {
        content
            .frame(maxWidth: sizeClass == .regular && !pad ? 680 : .infinity)
            .frame(maxWidth: .infinity)
            .navigationTitle(title)
            .modifier(PadPageTitle(title: title))
            .toolbar {
                if !store.stack.isEmpty, !chip {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Find", systemImage: "magnifyingglass") { find.show() }
                    }
                }
                PageVoiceItem()
                ToolbarItem(placement: .topBarTrailing) {
                    Menu("More", systemImage: "ellipsis") {
                        ShowInButton()
                        if !store.stack.isEmpty, chip {
                            Button("Find in Page", systemImage: "magnifyingglass") { find.show() }
                        }
                        Button("Copy Path", systemImage: "doc.on.doc") { UIPasteboard.general.string = path }
                    }
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
                if store.stack.isEmpty { await store.open(anchor.map { "\(path)#\($0)" } ?? path) }
            }
    }

    private var pad: Bool { PadLayout.on(sizeClass) }

    private var theme: DocumentTheme {
        var theme = DocumentTheme.system(dark: scheme == .dark)
        if pad {
            theme.top = 0
            theme.inset = PadLayout.margin
            theme.column = PadLayout.measure
        }
        return theme
    }

    private var title: String {
        store.stack.last?.title ?? URL(filePath: path).deletingPathExtension().lastPathComponent
    }

    @ViewBuilder
    private var content: some View {
        if let page = store.stack.last {
            DocumentWebView(content: DocumentContent(path: page.path, html: page.isArtifact ? page.html : SDUIPage(title: title, find: find).dropTitle(page.html), rev: page.rev, anchor: page.anchor, kind: page.isArtifact ? .artifact : .markdown), theme: theme, size: .fill, find: find, header: pad && !page.isArtifact ? AnyView(PageHeaderColumn(title: title, meta: sectionLine)) : nil, onScroll: reading?.scroll, onTextTap: reading?.tap, hardTopEdge: reading?.active == true, onLink: follow)
                .ignoresSafeArea(.container, edges: [.top, .bottom])
        } else if let error = store.error {
            ContentUnavailableView("Can't Open Document", systemImage: "doc.questionmark", description: Text(error))
        } else {
            ProgressView()
        }
    }

    private func follow(_ link: DocumentLink) {
        switch link {
        case .doc(let target, let fragment) where target == path:
            Task { await store.open(fragment.map { "\(target)#\($0)" } ?? target) }
        case .doc(let target, let fragment):
            navigation.openLink(target, anchor: fragment, from: Origin(tab: nil, title: title))
        case .external(let url):
            openURL(url)
        case .file, .anchor, .ignore:
            break
        }
    }
}

extension DocEntry {
    var symbol: String {
        isArtifact ? "chart.bar.doc.horizontal" : "doc.richtext"
    }
}
