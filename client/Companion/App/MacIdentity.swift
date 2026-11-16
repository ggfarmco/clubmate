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
                            inlineName(tag.name, presence(connection.presence))
                            inlineName(tag.name, presence(connection.shortPresence))
                            inlineName(tag.name, Text(connection.shortPresence))
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
                presence(connection.presence)
                    .font(.subheadline)
                    .textCase(nil)
                    .lineLimit(1)
            }
        }
    }
}

private extension MacSectionHeader {
    func presence(_ words: String) -> Text {
        guard connection.state == .online, let awake = connection.awake, awake.on else { return Text(words) }
        let gourd = Text(GourdGlyph.image(awake: true, size: 13)).baselineOffset(-2)
        guard !awake.ends.isEmpty else { return Text("\(words) · \(gourd)") }
        return Text("\(words) · \(gourd) \(awake.ends)")
    }
}

private func inlineName(_ name: String, _ presence: Text) -> some View {
    HStack(spacing: 6) {
        Text(name)
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
        Text("· \(presence)")
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
            if connection.state != .unauthorized, let awake = connection.awake {
                AwakeSection(connection: connection, awake: awake)
            }
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
                Button {
                    navigation.connectionSheet = ConnectionSheet(computer: computerID)
                } label: {
                    HStack(spacing: 8) {
                        Text("Connection")
                            .foregroundStyle(Color.primary)
                        Spacer(minLength: 8)
                        Text(connection.shortPresence)
                            .foregroundStyle(Color.captionInk)
                            .lineLimit(1)
                        Image(systemName: "chevron.right")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(Color(uiColor: .tertiaryLabel))
                    }
                    .contentShape(.rect)
                }
                .accessibilityIdentifier("mac.connection")
                Button {
                    navigation.reportSheet = ReportSheet(computer: computerID, about: nil)
                } label: {
                    Text("Report a Problem…")
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(.rect)
                }
                .accessibilityIdentifier("mac.report")
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

private struct AwakeSection: View {
    let connection: ComputerConnection
    let awake: AwakeState
    @Environment(AppNavigation.self) private var navigation
    @State private var sending = false
    @State private var failure: String?

    private var offline: Bool {
        connection.state != .online
    }

    var body: some View {
        Section {
            if offline {
                asleep
            } else {
                status
                ForEach(awake.agentHolds) { hold in
                    AwakeHoldRow(hold: hold) { open(hold) }
                }
            }
            picker
                .disabled(connection.state != .online || sending)
                .opacity(offline ? 0.45 : 1)
        } header: {
            Text(awake.labels?.section ?? "Keep Awake")
        } footer: {
            if let note = failure ?? (offline ? "The phone can't wake a sleeping Mac. Keep Awake can be changed again when it's back online." : awake.footnote) {
                Text(note)
                    .accessibilityIdentifier("mac.awake.footer")
            }
        }
    }

    private var status: some View {
        HStack(spacing: 12) {
            RowIcon(image: GourdGlyph.image(awake: awake.on, size: 16), tint: awake.on ? .accentColor : nil, fill: awake.on ? Color.accentColor.opacity(0.18) : nil)
            Text(awake.headline)
                .fontWeight(awake.on ? .semibold : .regular)
                .foregroundStyle(awake.on ? Color.primary : Color.captionInk)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityIdentifier("mac.awake.state")
        }
        .listRowDensity()
        .listRowSeparator(awake.agentHolds.isEmpty ? .automatic : .hidden, edges: .bottom)
    }

    private var asleep: some View {
        HStack(spacing: 12) {
            RowIcon(systemImage: "wifi.slash")
            VStack(alignment: .leading, spacing: 2) {
                Text("Asleep or offline")
                    .accessibilityIdentifier("mac.awake.state")
                Text(lastSeen)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .listRowDensity()
        .accessibilityElement(children: .combine)
    }

    private var lastSeen: String {
        var parts: [String] = []
        if let seen = connection.lastSeen { parts.append("Last seen " + ComputerConnection.clock(seen)) }
        if awake.on {
            parts.append(awake.ends.isEmpty ? "it was awake" : "it was awake " + awake.ends)
        }
        return parts.isEmpty ? "No news from the Mac yet" : parts.joined(separator: " · ")
    }

    @ViewBuilder
    private var picker: some View {
        if let labels = awake.labels {
            Menu {
                Section(labels.hours.title) {
                    ControlGroup {
                        ForEach(labels.hours.options) { option in
                            Toggle(isOn: Binding(get: { awake.mode == "hours" && awake.hours == option.hours }, set: { _ in set("hours", hours: option.hours) })) {
                                Text(option.short ?? option.title)
                                    .accessibilityLabel(option.title)
                            }
                        }
                    }
                    .controlGroupStyle(.palette)
                }
                Toggle(isOn: Binding(get: { awake.mode == "agents" }, set: { _ in set("agents") })) {
                    Text(labels.agents.title)
                    if let subtitle = labels.agents.subtitle { Text(subtitle) }
                }
                .accessibilityIdentifier("mac.awake.agents")
                Toggle(labels.on.title, isOn: Binding(get: { awake.mode == "on" }, set: { _ in set("on") }))
                    .accessibilityIdentifier("mac.awake.on")
                if awake.chosen {
                    Divider()
                    Button(labels.off.title) { set("off") }
                        .accessibilityIdentifier("mac.awake.off")
                }
            } label: {
                HStack(spacing: 8) {
                    Text(labels.picker ?? labels.section)
                        .foregroundStyle(Color.primary)
                    Spacer(minLength: 8)
                    if sending {
                        ProgressView()
                    } else {
                        Text(awake.value)
                            .foregroundStyle(Color.captionInk)
                            .lineLimit(1)
                            .accessibilityIdentifier("mac.awake.value")
                        Image(systemName: "chevron.up.chevron.down")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(Color.captionInk)
                    }
                }
                .padding(.leading, 40)
                .contentShape(.rect)
            }
            .accessibilityIdentifier("mac.awake.keep")
        }
    }

    private func open(_ hold: AwakeHold) {
        guard let session = hold.session else { return }
        let computer = connection.computer.id
        if let workspace = hold.workspace { navigation.workspaces[computer] = workspace }
        navigation.open(computer: computer, plugin: AgentsPluginInfo.id, detail: session)
    }

    private func set(_ mode: String, hours: Int? = nil) {
        sending = true
        failure = nil
        Task {
            do {
                try await connection.setAwake(mode: mode, hours: hours)
            } catch {
                failure = connection.failure(error, action: mode == "off" ? "turn it off" : "keep it awake")
            }
            sending = false
        }
    }
}

private struct AwakeHoldRow: View {
    let hold: AwakeHold
    let open: () -> Void

    var body: some View {
        if hold.session == nil {
            label
        } else {
            Button(action: open) { label }
                .buttonStyle(.plain)
                .accessibilityHint("Opens its session")
        }
    }

    private var label: some View {
        HStack(spacing: 10) {
            Image(systemName: "sparkle")
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(Color.captionInk)
                .frame(width: 24, height: 24)
                .background(Color(.tertiarySystemFill), in: .circle)
            VStack(alignment: .leading, spacing: 1) {
                Text(hold.title)
                    .font(.subheadline)
                    .foregroundStyle(Color.primary)
                    .lineLimit(1)
                if !hold.trailing.isEmpty {
                    Text(hold.trailing)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if hold.session != nil { RowChevron(always: true) }
        }
        .padding(.leading, 24)
        .listRowDensity(8)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("mac.awake.hold." + hold.id)
    }
}
