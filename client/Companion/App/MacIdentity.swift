import SwiftUI

import CompanionKit

extension MacColour {
    var color: Color { Color(light: light, dark: dark) }
}

extension MacTag {
    var color: Color { macColour.color }

    var dot: Text {
        Text(Image(systemName: "circle.fill")).font(.system(size: 8)).foregroundStyle(color)
    }

    func lead(_ rest: String) -> Text {
        rest.isEmpty ? Text("\(dot) \(name)") : Text("\(dot) \(name) · \(rest)")
    }
}

extension Optional where Wrapped == MacTag {
    func lead(_ rest: String) -> Text {
        map { $0.lead(rest) } ?? Text(rest)
    }
}

struct MacSubtitle: ToolbarContent {
    let text: Text?

    var body: some ToolbarContent {
        if let text {
            ToolbarItem(placement: .largeSubtitle) {
                text
                    .font(.subheadline)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

struct MacSectionHeader: View {
    let connection: ComputerConnection
    let tinted: Bool
    var inline = false
    let open: () -> Void

    var body: some View {
        let tag = connection.computer.tag
        HStack(spacing: 8) {
            Button(action: open) {
                HStack(spacing: 8) {
                    Image(systemName: tag.glyph)
                        .foregroundStyle(tinted ? tag.color : Color.captionInk)
                    if inline {
                        ViewThatFits(in: .horizontal) {
                            inlineName(tag.name, connection.presence)
                            inlineName(tag.name, connection.shortPresence)
                        }
                    } else {
                        Text(tag.name)
                            .lineLimit(1)
                    }
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .layoutPriority(1)
            .accessibilityHint("Shows this Mac's details")
            .accessibilityIdentifier("mac.header." + connection.computer.id)
            Spacer(minLength: inline ? 0 : 8)
            if !inline {
                Text(connection.presence)
                    .font(.subheadline)
                    .textCase(nil)
                    .lineLimit(1)
            }
        }
    }
}

private func inlineName(_ name: String, _ presence: String) -> some View {
    HStack(spacing: 6) {
        Text(name)
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
        Text("· " + presence)
            .font(.subheadline)
            .lineLimit(1)
            .fixedSize()
    }
    .textCase(nil)
}

struct MacDetailsView: View {
    let computerID: String
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @State private var choosing: MacColour?
    @State private var failure: String?
    @State private var forgetting = false

    var body: some View {
        if let connection = model.connection(computerID) {
            details(connection)
        } else {
            ContentUnavailableView("Mac Removed", systemImage: "desktopcomputer")
        }
    }

    private func details(_ connection: ComputerConnection) -> some View {
        let tag = connection.computer.tag
        let current = choosing ?? tag.macColour
        let others = model.computers.filter { $0.id != computerID }.map { model.connection($0.id)?.computer.tag ?? $0.tag }
        let reason = changeBlocked(connection)
        return List {
            Section {
                ForEach(MacColour.allCases) { colour in
                    Button {
                        choose(colour)
                    } label: {
                        HStack(spacing: 14) {
                            Circle()
                                .fill(colour.color)
                                .frame(width: 24, height: 24)
                            Text(colour.label)
                                .foregroundStyle(Color.primary)
                            Spacer(minLength: 8)
                            if colour == current {
                                Image(systemName: "checkmark")
                                    .font(.body.weight(.semibold))
                                    .foregroundStyle(.tint)
                            } else {
                                Text(others.filter { $0.macColour == colour }.map(\.name).joined(separator: ", "))
                                    .foregroundStyle(Color.captionInk)
                                    .lineLimit(1)
                                    .truncationMode(.tail)
                            }
                        }
                        .opacity(reason == nil ? 1 : 0.5)
                        .contentShape(.rect)
                    }
                    .listRowDensity(12)
                    .accessibilityAddTraits(colour == current ? .isSelected : [])
                    .accessibilityIdentifier("mac.colour." + colour.rawValue)
                }
            } header: {
                Text("Colour")
            } footer: {
                Text(failure ?? reason ?? "Shown next to this Mac's name on your iPhone and iPad, and in the Mac's title bar. Changing it here changes it on the Mac.")
            }
            .disabled(reason != nil || choosing != nil)
            Section("This Mac") {
                LabeledContent("Name", value: tag.name)
            }
            Section {
                Button("Forget this Mac", role: .destructive) { forgetting = true }
            }
        }
        .listStyle(.insetGrouped)
        .environment(\.defaultMinListRowHeight, 44)
        .navigationTitle(tag.name)
        .navigationSubtitle(summary(connection))
        .toolbar { MacSubtitle(text: model.namesMacs ? Text("\(tag.dot) \(summary(connection))") : nil) }
        .confirmationDialog("Forget \(tag.name)?", isPresented: $forgetting, titleVisibility: .visible) {
            Button("Forget", role: .destructive) {
                navigation.macDetails = nil
                if navigation.sidebar?.computer == computerID {
                    navigation.sidebar = nil
                    navigation.detail = nil
                }
                try? model.remove(computerID)
                navigation.arrange()
            }
        } message: {
            Text("This phone forgets the Mac and its access token. Revoke the device in Clubmate on the Mac to cut access there too.")
        }
    }

    private func summary(_ connection: ComputerConnection) -> String {
        let count = connection.workspaces.count
        guard connection.state == .online, count > 0 else { return connection.presence }
        return connection.presence + " · " + (count == 1 ? "1 workspace" : "\(count) workspaces")
    }

    private func changeBlocked(_ connection: ComputerConnection) -> String? {
        guard connection.state == .online else { return "Connect to change." }
        guard connection.hello?.computer.colour != nil else { return "Update Clubmate on this Mac to change its colour." }
        return nil
    }

    private func choose(_ colour: MacColour) {
        guard colour != model.connection(computerID)?.computer.tag.macColour else { return }
        choosing = colour
        failure = nil
        Task {
            do {
                try await model.setColour(colour, computer: computerID)
            } catch {
                failure = "Couldn't change the colour: " + ((error as? RemoteError)?.message ?? error.localizedDescription)
            }
            choosing = nil
        }
    }
}
