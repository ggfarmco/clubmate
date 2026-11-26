import SwiftUI

import ClubmateKit

struct DevicesSettings: View {
    let model: RemoteDevicesModel
    let client: CoreClient?
    let active: Bool

    @Environment(\.theme) private var theme

    var body: some View {
        Form {
            switch model.phase {
            case .loading:
                ProgressView().controlSize(.small)
            case .unavailable(let detail):
                unavailable(detail)
            case .ready:
                if let status = model.status, let computer = status.computer {
                    ThisMacSection(status: status, computer: computer, busy: model.busy) { name in
                        await model.setName(name)
                    } setColour: { colour in
                        Task { await model.setColour(colour) }
                    } setTitleBar: { enabled in
                        Task { await model.setTitleBarColour(enabled) }
                    }
                }
                if let status = model.status, status.enabled, let tunnel = status.tunnel {
                    RelaySection(relay: status.relay, tunnel: tunnel, busy: model.busy) { on in
                        Task { await model.setTunnel(on) }
                    } rotate: {
                        Task { await model.rotateRelayKey() }
                    }
                }
                status
                devices
            }
            if let error = model.error {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
            }
        }
        .formStyle(.grouped)
        .task(id: DevicesKey(client: client.map(ObjectIdentifier.init), active: active)) { await follow() }
        .sheet(isPresented: pairing) { PairDeviceSheet(model: model, presenter: RemoteDevicesModel.settingsPresenter) }
    }

    private func follow() async {
        guard active else { return }
        await model.load()
    }

    private func unavailable(_ detail: String) -> some View {
        Section {
            Label("Companion service unavailable", systemImage: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
            Text(detail)
                .font(.caption)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            Button("Retry") { Task { await model.load() } }
        }
    }

    @ViewBuilder private var status: some View {
        Section {
            SettingsToggle(title: "Allow companion devices", isOn: enabled)
                .disabled(model.busy || model.status == nil)
            if let status = model.status {
                let line = statusLine(status)
                HStack(spacing: theme.spacing.s) {
                    Circle().fill(line.dot).frame(width: 7, height: 7)
                    Text(line.text)
                        .foregroundStyle(line.ink)
                        .lineLimit(2)
                        .truncationMode(.label)
                        .help(line.text)
                        .textSelection(.enabled)
                }
                .font(.subheadline)
                if !model.pairHint.isEmpty {
                    SettingsDetail(model.pairHint)
                }
            }
            if let notice = model.notice {
                Label(notice, systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                    .lineLimit(1)
                    .truncationMode(.label)
            }
            SettingsRow(title: "Pair an iPhone or iPad") {
                Button("Pair Device…") { Task { await model.pair() } }
                    .disabled(model.busy)
            }
        }
    }

    @ViewBuilder private var devices: some View {
        Section("Devices") {
            if model.devices.isEmpty {
                VStack(alignment: .leading, spacing: theme.spacing.xs) {
                    Text("No devices paired yet").foregroundStyle(.secondary)
                    SettingsDetail("Pair Device… shows a code to scan with Clubmate on your iPhone or iPad. Works over Tailscale, or through the relay without it.")
                }
            }
            ForEach(model.devices) { device in
                DeviceRow(device: device, busy: model.busy) { Task { await model.revoke(device) } }
            }
        }
    }

    private var enabled: Binding<Bool> {
        Binding { model.status?.enabled ?? false } set: { value in Task { await model.setEnabled(value) } }
    }

    private var pairing: Binding<Bool> {
        Binding { model.presents(in: RemoteDevicesModel.settingsPresenter) } set: { if !$0 { Task { await model.done(in: RemoteDevicesModel.settingsPresenter) } } }
    }

    private func statusLine(_ status: RemoteStatus) -> (text: String, dot: Color, ink: Color) {
        guard let tailnet = status.tailnet else { return (status.summary, color(status.state), theme.foreground) }
        switch tailnet.severity {
        case .ok: return (tailnet.detail, theme.success, theme.foreground)
        case .note: return (tailnet.detail, theme.faint, theme.muted)
        case .warn: return (tailnet.detail, theme.warning, theme.foreground)
        case .error: return (tailnet.detail, theme.danger, theme.foreground)
        }
    }

    private func color(_ state: RemoteStatus.State) -> Color {
        switch state {
        case .listening: .green
        case .starting: .orange
        case .failed: .red
        case .off: .secondary
        }
    }
}

private struct ThisMacSection: View {
    let status: RemoteStatus
    let computer: RemoteComputer
    let busy: Bool
    let setName: (String) async -> Bool
    let setColour: (String) -> Void
    let setTitleBar: (Bool) -> Void

    @Environment(\.theme) private var theme
    @Environment(\.colorScheme) private var colorScheme
    @State private var name = ""
    @FocusState private var editing: Bool

    var body: some View {
        Section("This Mac") {
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                TextField("Name", text: $name, prompt: Text(status.systemName))
                    .multilineTextAlignment(.trailing)
                    .focused($editing)
                    .onSubmit(commitName)
                    .onChange(of: editing) { if !editing { commitName() } }
                    .onChange(of: computer.name, initial: true) { name = computer.name }
                    .accessibilityIdentifier("remote.name")
                    .disabled(busy)
                SettingsDetail(nameCaption)
            }
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                LabeledContent("Colour") {
                    HStack(spacing: theme.spacing.m) {
                        ForEach(Theme.identities, id: \.self) { token in
                            Swatch(token: token, selected: token == selected, takenBy: takenBy(token), busy: busy) { setColour(token) }
                        }
                    }
                }
                SettingsDetail(caption)
            }
            SettingsToggle(
                title: "Title bar",
                detail: status.titleBarColour && !status.titleDot ? "Show the colour in the workspace pill once a phone has another Mac paired" : "Show the colour in the workspace pill",
                isOn: Binding { status.titleBarColour } set: { setTitleBar($0) }
            )
            .disabled(busy)
            preview
        }
    }

    private var nameCaption: String {
        let system = status.systemName.isEmpty ? "the macOS computer name" : "“\(status.systemName)”"
        return "Shown on your iPhone and iPad and in their notifications. Leave it empty to use \(system)."
    }

    private func commitName() {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed != computer.name, !(trimmed.isEmpty && computer.name == status.systemName) else {
            name = computer.name
            return
        }
        let before = computer.name
        Task {
            if await !setName(trimmed) { name = before }
        }
    }

    private var selected: String? {
        computer.colour.isEmpty ? nil : Theme.identities.contains(computer.colour) ? computer.colour : "graphite"
    }

    private func takenBy(_ token: String) -> [String] {
        status.otherMacs.filter { (Theme.identities.contains($0.colour) ? $0.colour : "graphite") == token }.map(\.name)
    }

    private var caption: String {
        let shown = "Shown next to “\(computer.name)” on your iPhone and iPad."
        let own = selected.map { "\(Theme.identityName($0)). \(shown)" } ?? "No colour yet. \(shown)"
        let others = Theme.identities.compactMap { token -> String? in
            let names = takenBy(token)
            guard !names.isEmpty else { return nil }
            return "\(Theme.identityName(token).lowercased()) is \(names.formatted(.list(type: .and)))"
        }
        guard !others.isEmpty else { return own }
        let line = others.joined(separator: ", ")
        return own + "\n" + line.prefix(1).uppercased() + line.dropFirst() + " (marked •)."
    }

    private var preview: some View {
        Grid(alignment: .leading, horizontalSpacing: theme.spacing.xl, verticalSpacing: theme.spacing.l) {
            GridRow {
                Text("Mac").foregroundStyle(.secondary)
                TitlePill {
                    if status.titleBarColour { MacDot(colour: selected, dark: colorScheme == .dark) }
                    Text("clubmate")
                        .lineLimit(1)
                    Text("/").foregroundStyle(theme.faint)
                    Image(systemName: "arrow.triangle.branch").foregroundStyle(theme.muted)
                    Text("main").foregroundStyle(theme.foreground)
                }
                .fixedSize()
            }
            GridRow {
                Text("iPhone").foregroundStyle(.secondary)
                HStack(spacing: theme.spacing.m) {
                    MacDot(colour: selected, dark: colorScheme == .dark)
                    Text("\(computer.name) · clubmate · Run go test ./...?")
                        .lineLimit(1)
                        .truncationMode(.label)
                }
                .font(.system(size: theme.uiSize + 1))
                .foregroundStyle(theme.foreground)
                .padding(.horizontal, theme.spacing.l)
                .frame(height: theme.control)
                .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.background))
            }
        }
        .font(.caption)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Preview")
    }
}

private struct RelaySection: View {
    let relay: RemoteRelay?
    let tunnel: RemoteTunnel
    let busy: Bool
    let setOn: (Bool) -> Void
    let rotate: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        Section("Relay") {
            SettingsToggle(
                title: "Connect through Clubmate relay",
                detail: "Phones reach this Mac through Clubmate’s relay when Tailscale can’t. End‑to‑end encrypted.",
                isOn: Binding { tunnel.on } set: { setOn($0) }
            )
            .disabled(busy)
            if let relay, relay.mode == "manual", !relay.host.isEmpty {
                LabeledContent("Relay") {
                    Text(relay.host)
                        .lineLimit(1)
                        .truncationMode(.label)
                        .help(relay.url)
                        .textSelection(.enabled)
                }
            }
            if let line {
                HStack(spacing: theme.spacing.s) {
                    Circle().fill(line.colour).frame(width: 7, height: 7)
                    Text(line.text)
                        .lineLimit(2)
                        .truncationMode(.label)
                        .help(line.text)
                        .textSelection(.enabled)
                }
                .font(.subheadline)
            }
            if tunnel.on {
                SettingsRow(title: "Relay key") {
                    Button("Make a new relay key") { rotate() }
                        .fixedSize()
                        .disabled(busy)
                }
            }
        }
    }

    private var line: (text: String, colour: Color)? {
        if let relay, relay.state == "signing-up" || relay.state == "error", !relay.detail.isEmpty {
            return (relay.detail, relay.state == "error" ? Color.red : Color.orange)
        }
        guard !tunnel.detail.isEmpty else { return nil }
        return (tunnel.detail, colour(tunnel.state))
    }

    private func colour(_ state: String) -> Color {
        switch state {
        case "connected": .green
        case "connecting": .orange
        case "error": .red
        default: .secondary
        }
    }
}

private struct Swatch: View {
    let token: String
    let selected: Bool
    let takenBy: [String]
    let busy: Bool
    let choose: () -> Void

    @Environment(\.colorScheme) private var colorScheme

    private static let side: CGFloat = 18

    var body: some View {
        Button(action: choose) {
            Circle()
                .fill(colour)
                .frame(width: Self.side, height: Self.side)
                .overlay {
                    if selected {
                        Image(systemName: "checkmark")
                            .font(.system(size: Self.side / 2, weight: .bold))
                            .foregroundStyle(.background)
                    }
                }
                .padding(3)
                .overlay {
                    if selected { Circle().stroke(colour, lineWidth: 2) }
                }
                .overlay(alignment: .bottomTrailing) {
                    if !takenBy.isEmpty {
                        Circle()
                            .fill(.secondary)
                            .frame(width: 7, height: 7)
                            .overlay(Circle().stroke(.background, lineWidth: 1.5))
                    }
                }
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .disabled(busy)
        .help(help)
        .accessibilityLabel(help)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var colour: Color {
        Theme.identity(token, dark: colorScheme == .dark)
    }

    private var help: String {
        let name = Theme.identityName(token)
        return takenBy.isEmpty ? name : "\(name) — \(takenBy.formatted(.list(type: .and)))"
    }
}

private struct DevicesKey: Equatable {
    let client: ObjectIdentifier?
    let active: Bool
}

private struct DeviceRow: View {
    let device: RemoteDevice
    let busy: Bool
    let revoke: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.l) {
            VStack(alignment: .leading, spacing: theme.spacing.xs) {
                Text(device.title)
                    .fontWeight(.medium)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(device.title)
                TimelineView(.everyMinute) { context in
                    let detail = detail(context.date)
                    HStack(spacing: theme.spacing.s) {
                        Circle()
                            .fill(device.connected ? Color.green : Color.secondary.opacity(0.4))
                            .frame(width: 7, height: 7)
                        Text(detail)
                            .lineLimit(1)
                            .truncationMode(.label)
                            .help(detail)
                    }
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                }
            }
            .layoutPriority(1)
            Spacer(minLength: theme.spacing.l)
            Button("Revoke…", action: revoke)
                .fixedSize()
                .disabled(busy)
        }
    }

    private func detail(_ now: Date) -> String {
        let paired = "Paired \(device.pairedAt.formatted(date: .abbreviated, time: .omitted))"
        return [device.presence(now: now), device.model, paired].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

struct PairDeviceSheet: View {
    let model: RemoteDevicesModel
    let presenter: String

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xl) {
            VStack(alignment: .leading, spacing: theme.spacing.xs) {
                Text("Pair an iPhone or iPad")
                    .font(theme.heading)
                    .foregroundStyle(theme.foreground)
                Text("Open Clubmate on your iPhone or iPad, tap Scan code and point it at this code.")
                    .font(theme.ui)
                    .foregroundStyle(theme.text2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let pairing = model.pairing {
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    let left = pairing.countdown(now: context.date)
                    HStack(alignment: .top, spacing: theme.spacing.xl) {
                        code.opacity(left == nil ? 0.15 : 1)
                        VStack(alignment: .leading, spacing: theme.spacing.l) {
                            if let route = model.status?.companion?.route {
                                field(route.label, route.detail)
                            }
                            if !pairing.computerName.isEmpty {
                                field("This Mac", pairing.computerName)
                            }
                            if let left {
                                field("Code", "Expires in \(left)")
                            } else {
                                VStack(alignment: .leading, spacing: theme.spacing.s) {
                                    Text("Code expired")
                                        .font(theme.ui)
                                        .foregroundStyle(theme.danger)
                                    CardButton(label: "New code") { Task { await model.pair() } }
                                        .disabled(model.busy)
                                }
                            }
                        }
                        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                    }
                }
            } else if model.busy {
                ProgressView().controlSize(.small)
            }
            if let error = model.error {
                VStack(alignment: .leading, spacing: theme.spacing.xs) {
                    Text(error)
                        .foregroundStyle(theme.danger)
                        .textSelection(.enabled)
                    if !model.pairHint.isEmpty {
                        Text(model.pairHint).foregroundStyle(theme.muted)
                    }
                }
                .font(theme.uiSmall)
                .fixedSize(horizontal: false, vertical: true)
            }
            HStack {
                Spacer()
                CardButton(label: "Done", shortcut: "⎋", tone: .primary, isDefault: true) { close() }
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(EdgeInsets(top: theme.spacing.xxl, leading: theme.spacing.xxl, bottom: theme.spacing.xl, trailing: theme.spacing.xxl))
        .frame(width: 520, alignment: .topLeading)
        .background(theme.surface3)
        .onExitCommand { close() }
        .onDisappear { close() }
    }

    private func close() {
        Task { await model.done(in: presenter) }
    }

    private func field(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(label)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
            Text(value)
                .font(theme.ui)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.label)
                .help(value)
                .textSelection(.enabled)
        }
    }

    @ViewBuilder private var code: some View {
        Group {
            if let qr = model.qr {
                Image(decorative: qr, scale: 1)
                    .interpolation(.none)
                    .resizable()
            } else {
                Text("QR code unavailable")
                    .font(.caption)
                    .foregroundStyle(.black)
            }
        }
        .frame(width: 152, height: 152)
        .padding(8)
        .background(.white, in: RoundedRectangle(cornerRadius: theme.radius.l))
    }
}
