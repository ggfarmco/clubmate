import SwiftUI

import CompanionKit

struct MorePlugin: CompanionPluginUI {
    let id = MorePluginInfo.id
    let title = "More"
    let systemImage = "ellipsis.circle"
    let requiredCommands: [String] = []
    let tab: CompanionTab? = .more
    let needsUI = true

    @MainActor
    func makeRoot(context: PluginContext) -> AnyView {
        AnyView(MoreListView(context: context))
    }

    @MainActor
    func makeEmptyDetail(context: PluginContext) -> AnyView {
        if context.connection.state == .unauthorized {
            return AnyView(ConnectionUnavailableView(connection: context.connection))
        }
        return AnyView(ContentUnavailableView("No Section Selected", systemImage: "ellipsis.circle", description: Text("Pick Cron, Processes or another section to open it.")))
    }

    @MainActor
    func makeDestination(_ destination: Destination, context: PluginContext) -> AnyView? {
        switch destination {
        case .macDetails(let computer):
            return AnyView(MacDetailsView(computerID: computer))
        case .section(let id, let item) where id != InboxPluginInfo.id:
            guard context.connection.state != .unauthorized, let workspace = context.navigation.workspace(for: context.connection) else { return makeEmptyDetail(context: context) }
            return AnyView(SectionScreen(connection: context.connection, workspace: workspace.id, section: id, item: item).id("\(workspace.id)/\(id)/\(item ?? "")"))
        default:
            return nil
        }
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        0
    }
}

struct MoreListView: View {
    let context: PluginContext

    var body: some View {
        let workspace = context.navigation.workspace(for: context.connection)
        Group {
            if let workspace {
                MoreSectionsList(context: context, ui: context.connection.workspaceUI(workspace.id))
                    .id(workspace.id)
            } else if context.connection.state == .online {
                ContentUnavailableView("No Workspaces", systemImage: "folder", description: Text("Open a workspace in Clubmate on \(context.connection.computer.name)."))
            } else {
                ConnectionUnavailableView(connection: context.connection)
            }
        }
        .tabRootChrome(context, title: "More")
    }
}

private struct MoreSectionsList: View {
    let context: PluginContext
    @State private var ui: WorkspaceUI
    @State private var owner = UUID()
    @State private var queueFailure: String?

    init(context: PluginContext, ui: WorkspaceUI) {
        self.context = context
        _ui = State(initialValue: ui)
    }

    var body: some View {
        let sections = PhoneSections.more(ui.sections)
        List(selection: context.navigation.selection(.more)) {
            QueueSection(failure: $queueFailure)
            OfflineRow(connection: context.connection)
            Section {
                ForEach(sections, id: \.id) { section in
                    let selected = context.navigation.layout != .compact && context.navigation.selected(.more) == section.id
                    HStack(spacing: 12) {
                        RowIcon(systemImage: section.icon, tint: selected ? .accentColor : nil)
                        Text(section.title)
                            .lineLimit(1)
                    }
                    .frame(minHeight: 44)
                    .tag(section.id)
                    .columnRow(selected: selected)
                }
            }
            ThisMacSection(context: context)
        }
        .columnList()
        .actionFailure($queueFailure)
        .overlay {
            if ui.sections.isEmpty, let failure = ui.failure {
                ContentUnavailableView("Can't Load Sections", systemImage: "exclamationmark.triangle", description: Text(failure))
            } else if ui.sections.isEmpty {
                ProgressView()
            } else if sections.isEmpty {
                ContentUnavailableView("Nothing Else Here", systemImage: "ellipsis.circle", description: Text("Plugins with their own screens on the Mac show here."))
            }
        }
        .onAppear { ui.show([], owner: owner) }
        .onDisappear { ui.hide(owner: owner) }
    }
}

private struct ThisMacSection: View {
    let context: PluginContext

    private var subtitle: Text {
        let computer = context.connection.computer.id
        let presence = Text(context.connection.presence.prefix(1).uppercased() + context.connection.presence.dropFirst())
        guard let line = context.model.claudeAccounts.line(for: computer, now: .now) else { return presence }
        return presence + Text(" · usage ") + line.styled
    }

    var body: some View {
        let computer = context.connection.computer.id
        let destination = Destination.macDetails(computer: computer)
        let selected = context.navigation.layout != .compact && context.navigation.isTop(destination)
        Section {
            Button {
                context.navigation.setPath([StackEntry(destination)], tab: .more)
            } label: {
                HStack(spacing: 12) {
                    RowIcon(systemImage: "desktopcomputer", tint: selected ? .accentColor : nil)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("This Mac")
                        subtitle
                            .font(.subheadline)
                            .foregroundStyle(Color.captionInk)
                            .lineLimit(1)
                            .accessibilityIdentifier("more.thisMac.subtitle")
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    RowChevron()
                }
                .frame(minHeight: 44)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("more.thisMac")
            .columnRow(selected: selected)
        }
    }
}
