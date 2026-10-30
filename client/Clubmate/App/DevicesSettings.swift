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
        if let client {
            await model.follow(client)
        } else {
            await model.load()
        }
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
