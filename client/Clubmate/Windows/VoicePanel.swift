import AppKit
import SwiftUI

import ClubmateKit

struct VoicePanel: View {
    static let hideAfter = Duration.seconds(4)
    static let minWidth: CGFloat = 420
    static let privacySettings = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone")!

    let voice: VoiceController
    let openSession: (String) -> Void

    static func width(row: CGFloat) -> CGFloat {
        min(max(minWidth, row * 0.6), max(row - 32, 0))
    }

    @Environment(\.theme) private var theme
    @State private var keyFocus = false

    var body: some View {
        if voice.visible, let phase = voice.phase {
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                header(phase)
                heard(phase)
                if phase == .sending {
                    SendCountdown(voice: voice)
                }
                if let plan = voice.state?.plan, !plan.isEmpty {
                    Text(plan)
                        .foregroundStyle(theme.text2)
                        .lineLimit(3)
                        .help(plan)
                }
                if phase == .confirm, let confirm = voice.state?.confirm {
                    confirmation(confirm)
                }
                if phase == .done, let result = voice.state?.result, !result.isEmpty {
                    Text("✓ \(result)")
                        .foregroundStyle(theme.success)
                        .lineLimit(2)
                        .help(result)
                }
                if phase == .error, let error = voice.error {
                    failure(error)
                }
            }
            .font(theme.ui)
            .padding(theme.spacing.xl)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.surface3)
            .clipShape(RoundedRectangle(cornerRadius: theme.radius.xl))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.xl).stroke(theme.borderStrong, lineWidth: 1))
            .shadow(color: .black.opacity(0.35), radius: 16, y: 6)
            .background(ConfirmKeys(active: voice.acceptsConfirmKeys || voice.acceptsCancelKey, focused: $keyFocus) { approve, time in voice.confirmKey(approve, at: time) })
            .task(id: phase) {
                guard phase == .done || phase == .cancelled else { return }
                try? await Task.sleep(for: Self.hideAfter)
                if voice.phase == phase { voice.dismiss() }
            }
        }
    }

    private func header(_ phase: VoiceState.Phase) -> some View {
        HStack(spacing: theme.spacing.m) {
            Image(systemName: voice.active ? "mic.fill" : "mic")
                .foregroundStyle(voice.active ? theme.accentInk : theme.muted)
            Text("\(voice.title) · \(voice.status)")
                .foregroundStyle(theme.muted)
                .lineLimit(1)
            if let hint = voice.sendHint {
                LevelMeter(levels: voice.levels)
                Text(hint)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: theme.spacing.m)
            if voice.acceptsCancelKey {
                Text("Esc to cancel")
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            if let session = voice.state?.session, !voice.active {
                Button("Open Voice session") { openSession(session) }
                    .buttonStyle(.plain)
                    .foregroundStyle(theme.accentInk)
                    .lineLimit(1)
                    .help("Show the Voice session's transcript in Agents")
            }
            Button {
                voice.close()
            } label: {
                Image(systemName: "xmark")
                    .foregroundStyle(theme.muted)
                    .frame(width: 16, height: 16)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(voice.closeCancels ? "Cancel" : "Close")
        }
        .font(theme.uiSmall)
    }

    @ViewBuilder
    private func heard(_ phase: VoiceState.Phase) -> some View {
        let text = voice.heard
        if text.isEmpty {
            if voice.active {
                Text(phase == .loading ? "Keep talking, nothing is lost" : "Speak…")
                    .foregroundStyle(theme.faint)
            }
        } else {
            Text(text)
                .foregroundStyle(theme.foreground)
                .lineLimit(4)
                .truncationMode(.head)
                .help(text)
        }
    }

    private func confirmation(_ confirm: VoiceState.Confirm) -> some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            Text(confirm.title)
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
                .lineLimit(2)
                .help(confirm.title)
            ScrollView(.vertical) {
                Text(confirm.line)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, theme.spacing.l)
                    .padding(.vertical, theme.spacing.s)
            }
            .frame(maxHeight: theme.rowHeight * 6)
            .fixedSize(horizontal: false, vertical: true)
            .clipped()
            .help(confirm.line)
            .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.background))
                .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(theme.border, lineWidth: 1))
            HStack(spacing: theme.spacing.m) {
                CardButton(label: "Yes", shortcut: "↩", tone: .primary, disabled: !voice.acceptsConfirmKeys) { voice.confirmKey(true, at: ProcessInfo.processInfo.systemUptime) }
                    .overlay {
                        if keyFocus {
                            RoundedRectangle(cornerRadius: theme.radius.m + 2)
                                .stroke(theme.accent, lineWidth: theme.focusRing)
                                .padding(-3)
                        }
                    }
                CardButton(label: "No", shortcut: "esc", disabled: !voice.acceptsConfirmKeys) { voice.confirm(false) }
            }
        }
    }

    private func failure(_ error: String) -> some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            Text(error)
                .foregroundStyle(theme.danger)
                .lineLimit(3)
                .textSelection(.enabled)
                .help(error)
            if voice.micDenied {
                CardButton(label: "Open Privacy Settings") { NSWorkspace.shared.open(Self.privacySettings) }
            }
        }
    }
}

private struct SendCountdown: View {
    let voice: VoiceController

    @Environment(\.theme) private var theme

    var body: some View {
        TimelineView(.animation) { context in
            GeometryReader { geometry in
                Capsule()
                    .fill(theme.accent)
                    .frame(width: geometry.size.width * voice.sendRemaining(at: context.date))
            }
        }
        .frame(height: 3)
        .background(Capsule().fill(theme.border))
        .accessibilityHidden(true)
    }
}

private struct LevelMeter: View {
    let levels: [Double]

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(alignment: .center, spacing: 2) {
            ForEach(Array(levels.enumerated()), id: \.offset) { _, level in
                Capsule()
                    .fill(theme.accent)
                    .frame(width: 2, height: 2 + 10 * level)
            }
        }
        .frame(height: 12)
        .animation(.easeOut(duration: 0.15), value: levels)
        .accessibilityHidden(true)
    }
}

private struct ConfirmKeys: NSViewRepresentable {
    let active: Bool
    @Binding var focused: Bool
    let answer: @MainActor (Bool, TimeInterval) -> Void

    func makeNSView(context: Context) -> ConfirmKeyView {
        ConfirmKeyView()
    }

    func updateNSView(_ view: ConfirmKeyView, context: Context) {
        view.answer = answer
        view.focusChanged = { value in Task { @MainActor in focused = value } }
        view.active = active
    }
}

final class ConfirmKeyView: NSView {
    static let returnKeys: Set<UInt16> = [36, 76]
    static let escapeKey: UInt16 = 53

    var answer: (@MainActor (Bool, TimeInterval) -> Void)?
    var focusChanged: (@MainActor (Bool) -> Void)?
    var active = false {
        didSet {
            guard active != oldValue else { return }
            active ? take() : giveBack()
        }
    }

    private weak var previous: NSResponder?

    override var acceptsFirstResponder: Bool { active }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if active { take() }
    }

    override func mouseDown(with event: NSEvent) {
        guard active else { return super.mouseDown(with: event) }
        window?.makeFirstResponder(self)
    }

    override func becomeFirstResponder() -> Bool {
        focusChanged?(true)
        return true
    }

    override func resignFirstResponder() -> Bool {
        focusChanged?(false)
        return true
    }

    override func keyDown(with event: NSEvent) {
        let plain = event.modifierFlags.intersection([.command, .control, .option, .shift]).isEmpty
        guard active, plain else { return super.keyDown(with: event) }
        if Self.returnKeys.contains(event.keyCode) {
            if !event.isARepeat { answer?(true, event.timestamp) }
        } else if event.keyCode == Self.escapeKey {
            answer?(false, event.timestamp)
        } else {
            super.keyDown(with: event)
        }
    }

    private func take() {
        guard let window, window.firstResponder !== self else { return }
        previous = window.firstResponder
        window.makeFirstResponder(self)
    }

    private func giveBack() {
        guard let window, window.firstResponder === self else { return }
        if let previous, (previous as? NSView)?.window === window {
            window.makeFirstResponder(previous)
        } else {
            window.makeFirstResponder(nil)
        }
        previous = nil
    }
}
