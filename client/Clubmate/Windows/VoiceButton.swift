import AppKit
import SwiftUI

import ClubmateKit

struct VoiceButton: View {
    let voice: VoiceController

    @Environment(\.theme) private var theme

    var body: some View {
        Button {} label: {
            Image(systemName: voice.active ? "mic.fill" : "mic")
                .font(theme.uiSmall)
                .foregroundStyle(voice.active ? theme.accentInk : theme.muted)
                .frame(width: 16, height: 16)
                .background {
                    if voice.active {
                        Circle()
                            .stroke(theme.accent, lineWidth: 1.5)
                            .opacity(0.3 + 0.7 * voice.level)
                            .scaleEffect(1 + 0.3 * voice.level)
                            .animation(.easeOut(duration: 0.2), value: voice.level)
                    }
                }
                .contentShape(Rectangle())
        }
        .buttonStyle(VoicePressStyle(
            press: { voice.press(at: ProcessInfo.processInfo.systemUptime) },
            release: { voice.release(at: ProcessInfo.processInfo.systemUptime) }
        ))
        .help(voice.active ? "Stop listening (⌥Space)" : "Talk to Clubmate: click, or hold ⌥Space")
        .accessibilityLabel(voice.active ? "Stop listening" : "Talk to Clubmate")
    }
}

private struct VoicePressStyle: ButtonStyle {
    let press: () -> Void
    let release: () -> Void

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .onChange(of: configuration.isPressed) { _, pressed in
                pressed ? press() : release()
            }
    }
}

@MainActor
final class VoiceHotkeys {
    static let spaceKeyCode: UInt16 = 49

    private let target: @MainActor () -> VoiceController?
    private var monitor: Any?
    private var held: VoiceController?

    init(target: @escaping @MainActor () -> VoiceController?) {
        self.target = target
    }

    func install() {
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.keyDown, .keyUp]) { [weak self] event in
            MainActor.assumeIsolated { self?.handle(event) == true } ? nil : event
        }
        if TestMode.current { TestHarness.shared.voiceKeys = { [weak self] in self?.handle($0) == true } }
    }

    func handle(_ event: NSEvent) -> Bool {
        guard event.keyCode == Self.spaceKeyCode else { return false }
        if event.type == .keyUp {
            guard let voice = held else { return false }
            held = nil
            voice.release(at: event.timestamp)
            return true
        }
        if held != nil { return true }
        guard event.type == .keyDown, !event.isARepeat,
              event.modifierFlags.intersection([.command, .control, .option, .shift]) == .option,
              let voice = target() else { return false }
        held = voice
        voice.press(at: event.timestamp)
        return true
    }
}
