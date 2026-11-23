import SwiftUI

import CompanionKit

struct RootView: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @Environment(PairingFlow.self) private var pairing
    @Environment(VoiceSession.self) private var voice
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var size = CGSize.zero
    @State private var sectionUI: WorkspaceUI?
    @State private var tabsUI: WorkspaceUI?
    private static let sectionOwner = "root.section"
    private static let tabsOwner = "root.tabs"

    var body: some View {
        sheets(tracked(model.computers.isEmpty ? AnyView(WelcomeView()) : AnyView(RootShell())))
    }

    private func tracked(_ content: some View) -> some View {
        content
        .contentMargins(.bottom, navigation.floatingMic(model, listening: voice.active) ? VoiceButton.floating + 16 : 0, for: .scrollContent)
        .overlay { VoiceOverlay() }
        .background { MenuFeed(board: navigation.menu) }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in navigation.typing = true }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in navigation.typing = false }
        .onGeometryChange(for: CGSize.self) { $0.size } action: { size = $0 }
        .onChange(of: CompanionLayout(width: size.width, compactClass: sizeClass == .compact), initial: true) { _, layout in
            navigation.layout = layout
        }
        .onChange(of: ColumnStep.Room(width: size.width), initial: true) { _, room in navigation.room = room }
        .onChange(of: shownAgents, initial: true) { old, new in
            if let old, old.store != new?.store { model.agents(computer: old.store.computer, workspace: old.store.workspace)?.show(nil) }
            if let new { model.agents(computer: new.store.computer, workspace: new.store.workspace)?.show(new.detail) }
        }
        .onChange(of: shownSection, initial: true) { _, shown in
            sectionUI?.hide(owner: Self.sectionOwner)
            sectionUI = shown.flatMap { model.connection($0.computer)?.workspaceUI($0.workspace) }
            if let shown { sectionUI?.show([shown.section + ".sidebar", shown.section + ".main"], owner: Self.sectionOwner) }
        }
        .onChange(of: tabsSource, initial: true) { _, key in
            tabsUI?.hide(owner: Self.tabsOwner)
            tabsUI = key.flatMap { model.connection($0.computer)?.workspaceUI($0.workspace) }
            tabsUI?.show([], owner: Self.tabsOwner)
        }
        .onChange(of: navigation.liveTabs(model), initial: true) { _, live in
            if let live { navigation.remember(live) }
        }
        .onChange(of: fetched) { _, _ in navigation.prune() }
        .task { navigation.follow(model) }
        .onChange(of: model.computers) { _, _ in navigation.follow(model) }
        .onChange(of: workspaceIDs) { _, _ in navigation.follow(model) }
    }

    private func sheets(_ content: some View) -> some View {
        @Bindable var navigation = navigation
        return content
        .sheet(isPresented: $navigation.showingAddComputer, onDismiss: navigation.pairingSheetDismissed) {
            AddComputerView()
                .onAppear { navigation.pairingSheetAppeared() }
        }
        .sheet(item: Binding(get: { navigation.showingAddComputer ? nil : pairing.review }, set: { pairing.review = $0 }), onDismiss: navigation.pairingSheetDismissed) { review in
            PairingConfirmView(review: review)
                .onAppear { navigation.pairingSheetAppeared() }
        }
        .sheet(item: $navigation.connectionSheet) { sheet in
            ConnectionView(computerID: sheet.computer)
        }
        .sheet(item: Binding(get: { navigation.connectionSheet == nil ? navigation.reportSheet : nil }, set: { navigation.reportSheet = $0 })) { sheet in
            ReportView(sheet: sheet)
        }
        .sheet(item: $navigation.usageSheet) { sheet in
            ClaudeUsageView(computerID: sheet.computer)
        }
        .overlay {
            if case .pairing(let name) = pairing.phase {
                PairingOverlay(name: name)
            }
        }
        .alert("Couldn't Pair", isPresented: Binding(get: { pairing.failureMessage != nil && !navigation.showingAddComputer }, set: { if !$0 { pairing.dismissFailure() } })) {
            Button("OK") { pairing.dismissFailure() }
        } message: {
            Text(pairing.failureMessage ?? "")
        }
        .sensoryFeedback(.success, trigger: pairing.paired)
    }

    private var tabsSource: WorkspaceKey? {
        navigation.workspaceKey.flatMap { model.connection($0.computer) == nil ? nil : $0 }
    }

    private var workspaceIDs: [[String]] {
        model.computers.map { computer in model.connection(computer.id)?.workspaces.map(\.id) ?? [] }
    }

    private var fetched: [Bool] {
        guard let key = navigation.workspaceKey else { return [] }
        return [model.agents(computer: key.computer, workspace: key.workspace, settled: false)?.fetched == true, model.backlog(computer: key.computer, workspace: key.workspace, settled: false)?.fetched == true]
    }

    private var shownAgents: ShownAgents? {
        guard let current = navigation.workspaceKey else { return nil }
        let top = navigation.path.last { $0.destination.tab == .agents || navigation.tab == .agents && $0.destination.tab == .more }
        let key = top?.workspace ?? current
        let detail = top.flatMap { AppNavigation.tag($0.destination) }
        return ShownAgents(store: ShownAgents.Store(computer: key.computer, workspace: key.workspace), detail: detail)
    }

    private var shownSection: ShownSection? {
        guard let key = navigation.workspaceKey, case .section(let section, _)? = navigation.path.last?.destination,
              section != InboxPluginInfo.id, navigation.tab == .more || navigation.tab == .agents && section.hasPrefix(AgentsPluginInfo.id + ".") else { return nil }
        return ShownSection(computer: key.computer, workspace: key.workspace, section: section)
    }
}

struct RootShell: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    @State private var visited: Set<CompanionTab> = []

    var body: some View {
        shell
    }

    @ViewBuilder
    private var shell: some View {
        if navigation.layout == .compact {
            tabView
        } else if case .section(let current) = shown(navigation.root) {
            ZStack {
                ForEach(tabs.filter { visited.contains($0) || $0 == current }, id: \.self) { tab in
                    let active = tab == current
                    TabRoot(tab: tab)
                        .opacity(active ? 1 : 0)
                        .allowsHitTesting(active)
                        .accessibilityHidden(!active)
                        .zIndex(active ? 1 : 0)
                        .environment(\.tabShown, active)
                }
            }
            .onChange(of: current, initial: true) { _, tab in visited.insert(tab) }
        }
    }

    private var tabView: some View {
        TabView(selection: Binding(get: { shown(navigation.root) }, set: { pick in
            guard case .section(let tab) = pick else { return }
            navigation.pick(tab)
        })) {
            sections
        }
        .tabViewStyle(.tabBarOnly)
        .tabBarMinimizeBehavior(.onScrollDown)
    }

    @TabContentBuilder<RootTab>
    private var sections: some TabContent<RootTab> {
        ForEach(tabs, id: \.self) { tab in
            Tab(tab.title, systemImage: tab.systemImage, value: RootTab.section(tab)) {
                TabRoot(tab: tab)
            }
        }
    }

    private func shown(_ root: RootTab) -> RootTab {
        guard case .section(let tab) = root, !tabs.contains(tab), let first = tabs.first else { return root }
        return .section(first)
    }

    private var tabs: [CompanionTab] {
        navigation.tabs(model)
    }
}

struct TabRoot: View {
    let tab: CompanionTab
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        if let computer = navigation.computer, let connection = model.connection(computer), let plugin = CompanionPlugins.plugin(for: tab) {
            let context = PluginContext(model: model, connection: connection, navigation: navigation)
            if navigation.layout == .compact {
                NavigationStack(path: Binding(get: { navigation.path(tab) }, set: { navigation.setPath($0, tab: tab) })) {
                    plugin.makeRoot(context: context)
                        .navigationDestination(for: StackEntry.self) { entry in
                            DestinationView(entry: entry, tab: tab, context: context)
                        }
                }
                .toolbarVisibility(navigation.hidesTabBar(tab) ? .hidden : .automatic, for: .tabBar)
                .environment(\.ownerTab, tab)
            } else {
                NavigationSplitView(columnVisibility: columns) {
                    plugin.makeRoot(context: context)
                        .navigationSplitViewColumnWidth(ListColumn.width)
                        .toolbar { if tab == navigation.listTab { ListColumnTabs(tabs: navigation.tabs(model), owner: tab) } }
                } detail: {
                    ChatDock(tab: tab, connection: connection) {
                        TabDetail(tab: tab, plugin: plugin, context: context)
                            .background { SplitHidden() }
                    }
                }
                .modifier(SplitStyle(over: navigation.overlays(tab)))
                .environment(\.ownerTab, tab)
                .onChange(of: navigation.path(tab).first) { _, first in
                    if first != nil, tab == navigation.listTab { navigation.listOver = false }
                }
            }
        } else {
            ContentUnavailableView("Connecting", systemImage: "desktopcomputer", description: Text("Waiting for your Mac's workspaces."))
        }
    }

    private var columns: Binding<NavigationSplitViewVisibility> {
        Binding(get: { navigation.listShown(tab, shown: tab == navigation.listTab) ? .doubleColumn : .detailOnly }, set: { visibility in
            let shown = tab == navigation.listTab
            guard shown, visibility != .automatic, (visibility != .detailOnly) != navigation.listShown(tab, shown: shown) else { return }
            navigation.toggleList(tab)
        })
    }
}

struct ListColumnTabs: ToolbarContent {
    let tabs: [CompanionTab]
    let owner: CompanionTab
    @Environment(AppNavigation.self) private var navigation

    var body: some ToolbarContent {
        ToolbarItem(placement: .bottomBar) {
            HStack(spacing: 0) {
                ForEach(tabs, id: \.self) { tab in
                    button(tab)
                }
            }
        }
    }

    private func button(_ tab: CompanionTab) -> some View {
        let selected = tab == owner
        return Button {
            navigation.pick(tab)
        } label: {
            VStack(spacing: 2) {
                Image(systemName: tab.systemImage)
                    .font(.system(size: 18, weight: .medium))
                    .frame(height: 22)
                Text(tab.title)
                    .font(.system(size: 10, weight: .medium))
            }
            .foregroundStyle(selected ? Color.accentColor : Color.primary)
            .frame(minWidth: 56, minHeight: 44)
        }
        .accessibilityLabel(tab.title)
        .accessibilityHint("Tab")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

struct SplitHidden: UIViewRepresentable {
    func makeUIView(context: Context) -> Probe {
        Probe()
    }

    func updateUIView(_ probe: Probe, context: Context) {
        probe.concealed = !context.environment.tabShown
    }

    final class Probe: UIView {
        var concealed = false {
            didSet {
                guard concealed != oldValue else { return }
                apply()
                UIAccessibility.post(notification: .layoutChanged, argument: nil)
            }
        }

        override func didMoveToWindow() {
            super.didMoveToWindow()
            apply()
        }

        #if DEBUG
        private static let hosts = NSMapTable<UIView, UISplitViewController>.weakToWeakObjects()
        #endif

        private func apply() {
            guard let split = sequence(first: self as UIResponder, next: \.next).lazy.compactMap({ $0 as? UISplitViewController }).first else { return }
            if concealed { split.view.endEditing(true) }
            let host = split.view.superview.flatMap { $0.subviews == [split.view] ? $0 : nil }
            #if DEBUG
            if let host {
                assert(Self.hosts.object(forKey: host).map { $0 === split } ?? true, "SplitHidden: two split views share one host view")
                Self.hosts.setObject(split, forKey: host)
            }
            #endif
            for view in [split.view, host].compactMap({ $0 }) {
                view.isHidden = concealed
                view.accessibilityElementsHidden = concealed
                view.accessibilityElements = concealed ? [] : nil
            }
        }
    }
}

extension EnvironmentValues {
    @Entry var tabShown = true
    @Entry var ownerTab: CompanionTab?
    @Entry var stackEntry: StackEntry?
}

struct TabShownWatch: ViewModifier {
    let ui: WorkspaceUI
    let slots: Set<String>
    let owner: UUID
    @Environment(\.tabShown) private var shown
    @State private var appeared = false

    func body(content: Content) -> some View {
        content
            .onAppear {
                appeared = true
                if shown { ui.show(slots, owner: owner) }
            }
            .onDisappear {
                appeared = false
                ui.hide(owner: owner)
            }
            .onChange(of: shown) { _, shown in
                guard appeared else { return }
                if shown {
                    ui.show(slots, owner: owner)
                } else {
                    ui.hide(owner: owner)
                }
            }
    }
}

struct TabFetch: Hashable {
    let workspace: String
    let shown: Bool
}

struct SplitStyle: ViewModifier {
    let over: Bool

    func body(content: Content) -> some View {
        if over {
            content.navigationSplitViewStyle(.prominentDetail)
        } else {
            content.navigationSplitViewStyle(.balanced)
        }
    }
}

struct TabDetail: View {
    let tab: CompanionTab
    let plugin: any CompanionPluginUI
    let context: PluginContext
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        let path = navigation.path(tab)
        Group {
            if let first = path.first {
                NavigationStack(path: Binding(get: { Array(navigation.path(tab).dropFirst()) }, set: { navigation.setPath([first] + $0, tab: tab) })) {
                    DestinationView(entry: first, tab: tab, context: context)
                        .modifier(DetailCapsule(tab: tab, context: context))
                        .navigationDestination(for: StackEntry.self) { entry in
                            DestinationView(entry: entry, tab: tab, context: context)
                        }
                }
                .id(first)
            } else {
                plugin.makeEmptyDetail(context: context)
                    .modifier(DetailCapsule(tab: tab, context: context))
            }
        }
        .onChange(of: path.first, initial: true) { _, first in
            guard tab == .wiki, let first, case .wikiPage = first.destination, let key = navigation.workspaceKey else { return }
            navigation.lastWiki[key] = first
        }
    }
}

struct DestinationView: View {
    let entry: StackEntry
    let tab: CompanionTab
    let context: PluginContext
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @State private var reading = false

    var body: some View {
        let context = entry.workspace.flatMap { key in model.connection(key.computer).map { PluginContext(model: model, connection: $0, navigation: navigation, workspace: key.workspace) } } ?? context
        (CompanionPlugins.destination(entry.destination, tab: tab, context: context) ?? AnyView(ContentUnavailableView("Not Found", systemImage: "questionmark")))
            .frame(maxWidth: readingView && entry.destination.isSession ? 680 : .infinity)
            .frame(maxWidth: .infinity)
            .onPreferenceChange(ReadingActiveKey.self) { reading = $0 }
            .onChange(of: reading) { _, reading in navigation.reading(reading, entry) }
            .environment(\.readingView, readingView)
            .modifier(ReadingViewBars(hidden: readingView, owned: entry.destination.readsInPlace))
            .modifier(BackChipModifier(entry: entry, tab: tab))
            .modifier(SectionLineModifier(entry: entry, tab: tab))
            .environment(\.stackEntry, entry)
    }

    private var readingView: Bool {
        navigation.readingView && navigation.tab == tab && navigation.layout != .compact
    }
}

struct ReadingViewBars: ViewModifier {
    let hidden: Bool
    let owned: Bool

    func body(content: Content) -> some View {
        if owned {
            content
        } else {
            content.toolbarVisibility(hidden ? .hidden : .automatic, for: .navigationBar)
        }
    }
}

extension Destination {
    var readsInPlace: Bool {
        switch self {
        case .wikiPage, .doc: true
        default: false
        }
    }

    var isSession: Bool {
        if case .session = self { return true }
        return false
    }
}

extension CompanionTab {
    var systemImage: String {
        switch self {
        case .agents: "sparkles"
        case .backlog: "checklist"
        case .wiki: "book"
        case .more: "ellipsis.circle"
        }
    }
}

struct ShownAgents: Equatable {
    struct Store: Equatable {
        let computer: String
        let workspace: String
    }

    let store: Store
    let detail: String?
}

struct ShownSection: Equatable {
    let computer: String
    let workspace: String
    let section: String
}

struct PairingOverlay: View {
    let name: String

    var body: some View {
        VStack(spacing: 14) {
            ProgressView()
                .controlSize(.large)
            Text("Pairing with \(name)…")
                .font(.headline)
        }
        .padding(28)
        .glassEffect(.regular, in: .rect(cornerRadius: 28))
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.black.opacity(0.15))
    }
}

struct WelcomeView: View {
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        ContentUnavailableView {
            Label("Pair a Mac", systemImage: "macbook.and.iphone")
        } description: {
            Text("On your Mac, open Clubmate Settings › Remote › Pair Device…, then scan the code.")
            Text("Works over Tailscale, or through the relay without it.")
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
        } actions: {
            Button("Scan code", systemImage: "qrcode.viewfinder") {
                navigation.scanOnPair = true
                navigation.showingAddComputer = true
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            VStack(spacing: 2) {
                Text("No Clubmate on your Mac yet?")
                    .foregroundStyle(Color.captionInk)
                Link("github.com/ggfarmco/clubmate", destination: AddComputerView.macDownload)
            }
            .font(.footnote)
            .padding(.top, 12)
        }
    }
}
