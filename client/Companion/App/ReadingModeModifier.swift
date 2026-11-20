import SwiftUI

import ClubmateDocument
import CompanionKit

struct DocumentReading {
    let scroll: (DocumentScroll) -> Void
    let tap: () -> Void
    let active: Bool
    let titled: Bool
}

extension EnvironmentValues {
    @Entry var documentReading: DocumentReading?
    @Entry var readingView = false
}

struct ReadingActiveKey: PreferenceKey {
    static let defaultValue = false

    static func reduce(value: inout Bool, nextValue: () -> Bool) {
        value = value || nextValue()
    }
}

@MainActor
private final class ReadingState {
    var mode = ReadingMode()
}

struct ReadingModeModifier: ViewModifier {
    @State private var state = ReadingState()
    @State private var active = false
    @State private var titled = false
    @State private var progress = 0.0
    @State private var top: CGFloat = 0
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOver
    @Environment(\.accessibilitySwitchControlEnabled) private var switchControl
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.readingView) private var readingView

    private var assistive: ReadingMode.Assistive {
        var assistive: ReadingMode.Assistive = []
        if voiceOver { assistive.insert(.voiceOver) }
        if switchControl { assistive.insert(.switchControl) }
        if reduceMotion { assistive.insert(.reduceMotion) }
        return assistive
    }

    func body(content: Content) -> some View {
        content
            .environment(\.documentReading, DocumentReading(scroll: scrolled, tap: { handle(.tappedText) }, active: active, titled: titled))
            .toolbarVisibility(active || readingView ? .hidden : .automatic, for: .navigationBar)
            .preference(key: ReadingActiveKey.self, value: active)
            .overlay(alignment: .top) {
                if active {
                    VStack(spacing: 0) {
                        Color.clear.frame(height: PadLayout.on(sizeClass) ? max(0, statusBar - top) : 58)
                        line
                    }
                    .ignoresSafeArea(edges: .top)
                    .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minY } action: { top = $0 }
                }
            }
            .background {
                if active {
                    Button("Leave Reading Mode") { handle(.escape) }
                        .keyboardShortcut(.cancelAction)
                        .opacity(0)
                        .accessibilityHidden(true)
                }
            }
            .onChange(of: assistive, initial: true) { _, value in handle(.accessibility(value)) }
            .onDisappear { handle(.escape) }
    }

    private var statusBar: CGFloat {
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.statusBarManager?.statusBarFrame.height }.max() ?? 24
    }

    private var line: some View {
        VStack(spacing: 0) {
            Rectangle()
                .fill(Color(.tertiaryLabel))
                .frame(height: 2)
                .scaleEffect(x: progress, anchor: .leading)
            Spacer(minLength: 0)
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
        .transition(.opacity)
    }

    private func scrolled(_ scroll: DocumentScroll) {
        let pad = PadLayout.on(sizeClass)
        if pad, (scroll.offset >= PageHeaderColumn.titleEnd) != titled { titled.toggle() }
        guard scroll.tracking, sizeClass == .compact || pad else { return }
        handle(.scrolled(offset: scroll.offset, delta: scroll.delta))
        if active { progress = scroll.progress }
    }

    private func handle(_ input: ReadingMode.Input) {
        state.mode.handle(input)
        guard state.mode.active != active else { return }
        withAnimation(state.mode.active ? .easeOut(duration: 0.25) : .spring(response: 0.3, dampingFraction: 1)) {
            active = state.mode.active
        }
    }
}

extension View {
    func readingMode() -> some View {
        modifier(ReadingModeModifier())
    }
}
