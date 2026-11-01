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
                    ThisMacSection(status: status, computer: computer, busy: model.busy) { colour in
                        Task { await model.setColour(colour) }
                    } setTitleBar: { enabled in
                        Task { await model.setTitleBarColour(enabled) }
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
        .sheet(isPresented: pairing) { PairDeviceSheet(model: model) }
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
            Toggle("Allow companion devices", isOn: enabled)
                .disabled(model.busy || model.status == nil)
            if let status = model.status {
                HStack(spacing: theme.spacing.s) {
                    Circle().fill(color(status.state)).frame(width: 7, height: 7)
                    Text(status.summary)
                        .lineLimit(2)
                        .truncationMode(.label)
                        .help(status.summary)
                        .textSelection(.enabled)
                }
                .font(.caption)
                if model.needsTailscale {
                    Text(RemoteDevicesModel.tailscaleHint)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            if let notice = model.notice {
                Label(notice, systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                    .lineLimit(1)
                    .truncationMode(.label)
            }
            LabeledContent("Pair an iPhone or iPad") {
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
                    Text("Install Clubmate on your iPhone or iPad. Both devices must be on the same Tailscale tailnet.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
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
        Binding { model.pairing != nil } set: { if !$0 { Task { await model.done() } } }
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
    let setColour: (String) -> Void
    let setTitleBar: (Bool) -> Void

    @Environment(\.theme) private var theme
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        Section("This Mac") {
            LabeledContent("Name") {
                Text(computer.name)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(computer.name)
                    .textSelection(.enabled)
            }
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                LabeledContent("Colour") {
                    HStack(spacing: theme.spacing.m) {
                        ForEach(Theme.identities, id: \.self) { token in
                            Swatch(token: token, selected: token == selected, takenBy: takenBy(token), busy: busy) { setColour(token) }
                        }
                    }
                }
                Text(caption)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Toggle(isOn: Binding { status.titleBarColour } set: { setTitleBar($0) }) {
                Text("Title bar")
                Text(status.titleBarColour && !status.titleDot ? "Show the colour in the workspace pill once a phone has another Mac paired" : "Show the colour in the workspace pill")
            }
            .disabled(busy)
            preview
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
                    .font(.caption)
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

private struct PairDeviceSheet: View {
    let model: RemoteDevicesModel

    var body: some View {
        VStack(spacing: 12) {
            Text("Pair Device").font(.headline)
            Text("Scan this code with Clubmate on your iPhone or iPad.")
                .font(.caption)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            if let pairing = model.pairing {
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    let left = pairing.countdown(now: context.date)
                    VStack(spacing: 8) {
                        code.opacity(left == nil ? 0.15 : 1)
                        if !pairing.computerName.isEmpty {
                            Text(pairing.computerName)
                                .fontWeight(.medium)
                                .lineLimit(1)
                                .truncationMode(.label)
                                .help(pairing.computerName)
                        }
                        Text(left.map { "Expires in \($0)" } ?? "Code expired. Choose New Code.")
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(left == nil ? Color.red : Color.secondary)
                    }
                }
            }
            if let error = model.error {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
                if model.needsTailscale {
                    Text(RemoteDevicesModel.tailscaleHint)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            HStack {
                Button("New Code") { Task { await model.pair() } }
                    .disabled(model.busy)
                Spacer()
                Button("Done") { Task { await model.done() } }
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(20)
        .frame(width: 340)
        .onExitCommand { Task { await model.done() } }
        .onDisappear { Task { await model.done() } }
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
        .frame(width: 240, height: 240)
        .padding(10)
        .background(.white, in: RoundedRectangle(cornerRadius: 8))
    }
}
