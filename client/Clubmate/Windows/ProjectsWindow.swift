import AppKit
import SwiftUI

import ClubmateKit

struct ProjectsWindow: View {
    @Environment(AppModel.self) private var app
    @Environment(EventBanner.self) private var banner: EventBanner?
    @Environment(\.theme) private var theme
    @Environment(\.openWindow) private var openWindow
    @Environment(\.dismissWindow) private var dismissWindow
    @State private var selection: String?
    @FocusState private var listFocused: Bool

    var body: some View {
        content
            .frame(minWidth: 480, minHeight: 360)
            .background(theme.background, ignoresSafeAreaEdges: .all)
            .task {
                app.launch()
                guard let id = app.takeLaunchProject() else { return }
                openWindow(value: id)
                dismissWindow(id: "projects")
            }
    }

    @ViewBuilder private var content: some View {
        switch app.phase {
        case .starting:
            EmptyNodeView(text: "Starting core…")
        case .failed(let message):
            VStack(spacing: theme.gutter * 1.5) {
                Text(message)
                    .font(theme.ui)
                    .foregroundStyle(theme.danger)
                    .multilineTextAlignment(.center)
                    .textSelection(.enabled)
                Button("Retry") { app.retry() }
            }
            .padding(theme.gutter * 3)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .ready, .reconnecting:
            VStack(spacing: 0) {
                header
                theme.border.frame(height: 1)
                list
            }
        }
    }

    private var header: some View {
        HStack {
            Text("Projects")
                .font(theme.title)
                .foregroundStyle(theme.foreground)
            Spacer()
            Button(action: newProject) {
                Label("New Project…", systemImage: "plus")
                    .font(theme.ui)
                    .foregroundStyle(theme.foreground)
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, theme.gutter * 1.5)
        .frame(height: 36)
    }

    @ViewBuilder private var list: some View {
        let projects = app.projects?.projects ?? []
        if projects.isEmpty {
            EmptyNodeView(text: "No projects")
        } else {
            List(projects, id: \.id, selection: $selection) { project in
                VStack(alignment: .leading, spacing: 2) {
                    Text(project.name)
                        .font(theme.ui)
                        .foregroundStyle(theme.foreground)
                    if !project.repos.isEmpty {
                        Text(project.repos.joined(separator: ", "))
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                }
                .padding(.vertical, 3)
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .contextMenu(forSelectionType: String.self) { _ in
                EmptyView()
            } primaryAction: { ids in
                ids.first.map { openWindow(value: $0) }
            }
            .focused($listFocused)
            .onAppear { listFocused = true }
            .onChange(of: projects.map(\.id), initial: true) { _, ids in
                if selection.map(ids.contains) != true { selection = ids.first }
            }
        }
    }

    private func newProject() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.prompt = "Create Project"
        guard panel.runModal() == .OK, let first = panel.urls.first else { return }
        let repos = panel.urls.map(\.path)
        Task {
            do {
                let project = try await app.createProject(name: first.lastPathComponent, repos: repos)
                openWindow(value: project.id)
            } catch {
                banner?.show((error as? ClubmateError)?.bannerText ?? "\(error)")
            }
        }
    }
}
