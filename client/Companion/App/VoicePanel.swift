import SwiftUI
import UIKit

import CompanionKit

struct VoicePanel: View {
    static let hideAfter = Duration.seconds(4)

    @Environment(VoiceSession.self) private var voice
    @Environment(AppNavigation.self) private var navigation
    @Environment(\.openURL) private var openURL

    var body: some View {
        if voice.visible, let phase = voice.phase {
            ViewThatFits(in: .vertical) {
                content(phase)
                ScrollView {
                    content(phase)
                }
                .scrollBounceBehavior(.basedOnSize)
            }
            .background(Color.surface1, in: .rect(cornerRadius: 20))
            .overlay(RoundedRectangle(cornerRadius: 20).strokeBorder(phase == .confirm ? Color.warning.opacity(0.55) : Color(.separator), lineWidth: 1))
            .shadow(color: .black.opacity(0.25), radius: 16, y: 6)
            .transition(.move(edge: .bottom).combined(with: .opacity))
            .task(id: phase) {
                guard phase == .done || phase == .cancelled else { return }
                try? await Task.sleep(for: Self.hideAfter)
                if voice.phase == phase { voice.dismiss() }
            }
        }
    }

    private func content(_ phase: VoiceState.Phase) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            header
            heard(phase)
            if let plan = voice.state?.plan, !plan.isEmpty {
                Text(plan)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
            }
            if phase == .confirm, let confirm = voice.state?.confirm {
                confirmation(confirm)
            }
            if phase == .done, let result = voice.state?.result, !result.isEmpty {
                Label(result, systemImage: "checkmark")
                    .font(.subheadline)
                    .lineLimit(2)
                    .labelStyle(VoiceResultLabel())
            }
            if phase == .error, let error = voice.error {
                failure(error)
            }
            if let session = voice.state?.session, let target = voice.target, !voice.active {
                Button("Open Voice session") {
                    navigation.workspaces[target.computer] = target.workspace
                    navigation.open(computer: target.computer, plugin: AgentsPluginInfo.id, detail: session)
                    voice.dismiss()
                }
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.tint)
                .frame(minHeight: 44)
                .padding(.vertical, -6)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var header: some View {
        HStack(spacing: 8) {
            Image(systemName: voice.active ? "mic.fill" : "mic")
                .foregroundStyle(voice.active ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
            Text("Voice · \(voice.status)")
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Spacer(minLength: 8)
            Button {
                voice.active ? voice.cancel() : (voice.phase == .confirm ? voice.confirm(false) : voice.dismiss())
            } label: {
                Image(systemName: "xmark")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 30, height: 30)
                    .background(Color.surface2, in: .circle)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .padding(.vertical, -12)
            .padding(.trailing, -8)
            .accessibilityLabel(voice.active ? "Cancel" : "Close")
        }
        .font(.subheadline)
    }

    @ViewBuilder
    private func heard(_ phase: VoiceState.Phase) -> some View {
        let text = voice.heard
        if text.isEmpty {
            if voice.active {
                Text(phase == .loading ? "Keep talking, nothing is lost" : "Speak…")
                    .foregroundStyle(.tertiary)
            }
        } else {
            Text(text)
                .lineLimit(4)
                .truncationMode(.head)
        }
        if voice.active {
            HStack(spacing: 8) {
                LevelMeter(levels: voice.levels)
                Text(voice.hint)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
    }

    private func confirmation(_ confirm: VoiceState.Confirm) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 12) {
                DockBadge(symbol: "exclamationmark")
                Text(confirm.title)
                    .font(.headline)
                    .lineLimit(2)
            }
            ScrollView(.vertical) {
                Text(confirm.line)
                    .font(.callout.monospaced())
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
            }
            .frame(maxHeight: 132)
            .fixedSize(horizontal: false, vertical: true)
            .background(Color(.systemGroupedBackground), in: .rect(cornerRadius: 16))
            .clipShape(.rect(cornerRadius: 16))
            HStack(spacing: 10) {
                Button { voice.answer(true, at: ProcessInfo.processInfo.systemUptime) } label: {
                    Text("Yes")
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 50)
                        .foregroundStyle(Color.onAccent)
                        .background(Color.accentColor, in: .capsule)
                        .contentShape(.capsule)
                }
                .buttonStyle(.plain)
                .keyboardShortcut(.return, modifiers: .option)
                PlainCapsule(title: "No", tint: .primary) { voice.confirm(false) }
                    .frame(minHeight: 50)
                    .keyboardShortcut(.cancelAction)
            }
            .disabled(!voice.acceptsConfirm)
            .opacity(voice.acceptsConfirm ? 1 : 0.5)
        }
    }

    private func failure(_ error: String) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(error)
                .font(.subheadline)
                .foregroundStyle(Color.dangerInk)
                .lineLimit(3)
                .textSelection(.enabled)
            if voice.micDenied, let settings = URL(string: UIApplication.openSettingsURLString) {
                PlainCapsule(title: "Open Settings", tint: .primary) { openURL(settings) }
            }
        }
    }
}

private struct VoiceResultLabel: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            configuration.icon
                .foregroundStyle(.secondary)
            configuration.title
        }
    }
}

private struct LevelMeter: View {
    let levels: [Double]

    var body: some View {
        HStack(alignment: .center, spacing: 2) {
            ForEach(Array(levels.enumerated()), id: \.offset) { _, level in
                Capsule()
                    .fill(.tint)
                    .frame(width: 2, height: 2 + 12 * level)
            }
        }
        .frame(height: 14)
        .animation(.easeOut(duration: 0.15), value: levels)
        .accessibilityHidden(true)
    }
}
