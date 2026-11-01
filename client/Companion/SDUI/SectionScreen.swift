import SwiftUI

import CompanionKit

struct SectionScreen: View {
    let section: String
    @State private var ui: WorkspaceUI
    @State private var owner = UUID()
    @State private var path: [String] = []
    @State private var error: String?

    init(connection: ComputerConnection, workspace: String, section: String) {
        self.section = section
        _ui = State(initialValue: connection.workspaceUI(workspace))
    }

    var body: some View {
        let sidebar = "\(section).sidebar"
        let main = "\(section).main"
        NavigationStack(path: $path) {
            Group {
                if let root = PhoneSections.root(section, sidebar: ui.view(sidebar), main: ui.view(main)) {
                    slot(root, open: root == sidebar ? { _ in path = [main] } : { _ in })
                } else if let failure = ui.failure {
                    ContentUnavailableView("Can't Load \(title)", systemImage: "exclamationmark.triangle", description: Text(failure))
                } else {
                    ProgressView()
                }
            }
            .navigationTitle(title)
            .navigationDestination(for: String.self) { slot in
                self.slot(slot, open: { _ in })
                    .navigationTitle(title)
                    .navigationBarTitleDisplayMode(.inline)
            }
        }
        .alert("Couldn't Do That", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button("OK") { error = nil }
        } message: {
            Text(error ?? "")
        }
        .onAppear { ui.show([sidebar, main], owner: owner) }
        .onDisappear { ui.hide(owner: owner) }
    }

    private var title: String {
        ui.sections.first { $0.id == section }?.title ?? section
    }

    @ViewBuilder
    private func slot(_ slot: String, open: @escaping (String) -> Void) -> some View {
        if let view = ui.view(slot) {
            SDUINodeView(node: view.tree, context: SDUIContext(slot: slot, send: { nodeId, action, value in
                do {
                    _ = try await ui.send(slot: slot, nodeId: nodeId, action: action, value: value)
                } catch {
                    self.error = (error as? RemoteError)?.message ?? error.localizedDescription
                }
            }, open: open))
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        } else {
            ProgressView()
        }
    }
}

struct WorkspacePopups: ViewModifier {
    let connection: ComputerConnection?
    let workspace: String?
    @State private var ui: WorkspaceUI?
    @State private var owner = UUID()
    @State private var presented: PopupSlot?
    @State private var error: String?

    func body(content: Content) -> some View {
        content
            .sheet(item: $presented) { popup in
                if let ui {
                    PopupSheet(ui: ui, slot: popup.slot) { presented = nil }
                }
            }
            .onChange(of: workspace.flatMap { workspace in connection.map { "\($0.computer.id)/\(workspace)" } }, initial: true) { _, _ in
                ui?.hide(owner: owner)
                ui = workspace.flatMap { workspace in connection?.workspaceUI(workspace) }
                ui?.show([], owner: owner)
            }
            .onChange(of: ui?.popup, initial: true) { _, slot in
                presented = slot.map(PopupSlot.init)
            }
            .onChange(of: presented) { old, new in
                guard let ui, let old, new == nil, ui.popup == old.slot else { return }
                Task {
                    do {
                        try await ui.dismissPopup(old.slot)
                    } catch {
                        self.error = (error as? RemoteError)?.message ?? error.localizedDescription
                    }
                }
            }
            .alert("Couldn't Close", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("OK") { error = nil }
            } message: {
                Text(error ?? "")
            }
    }
}

private struct PopupSlot: Identifiable, Equatable {
    let slot: String
    var id: String { slot }
}

private struct PopupSheet: View {
    let ui: WorkspaceUI
    let slot: String
    let close: () -> Void
    @State private var error: String?

    var body: some View {
        let tree = ui.view(slot)?.tree
        NavigationStack {
            Group {
                if let tree {
                    SDUINodeView(node: tree, context: SDUIContext(slot: slot, send: { nodeId, action, value in
                        do {
                            _ = try await ui.send(slot: slot, nodeId: nodeId, action: action, value: value)
                        } catch {
                            self.error = (error as? RemoteError)?.message ?? error.localizedDescription
                        }
                    }, open: { _ in }))
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                }
            }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done", systemImage: "checkmark", action: close)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .alert("Couldn't Do That", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("OK") { error = nil }
            } message: {
                Text(error ?? "")
            }
        }
        .presentationDetents(tree.map(Self.hasDocument) == true ? [.large] : [.medium, .large])
    }

    private static func hasDocument(_ node: SDUINode) -> Bool {
        node.type == "document" || node.children.contains(where: hasDocument)
    }
}
