import SwiftUI

import CompanionKit

struct VoiceOverlay: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @Environment(VoiceSession.self) private var voice

    static let margin: CGFloat = 12
    static let panelWidth: CGFloat = 480

    var body: some View {
        let target = navigation.floatingMic(model) ? navigation.voiceTarget(model) : nil
        GeometryReader { proxy in
            let frame = proxy.frame(in: .global)
            let column = navigation.layout == .compact ? frame : navigation.voiceDock.map { $0.intersection(frame) } ?? frame
            let room = column.isNull ? frame.width : column.width
            let width = navigation.layout == .compact ? room - 2 * Self.margin : min(Self.panelWidth, room - 2 * Self.margin)
            let button = clearance(in: frame, across: VoiceButton.floating + 2 * Self.margin)
            let panel = max(target == nil ? button : button + VoiceButton.floating + 10, clearance(in: frame, across: .infinity))
            ZStack(alignment: .bottomLeading) {
                VoicePanel()
                    .frame(width: max(width, 0))
                    .padding(.leading, column.isNull ? 0 : column.minX - frame.minX)
                    .padding(.bottom, panel)
                if let target {
                    VoiceButton(size: VoiceButton.floating, online: model.connection(target.computer)?.state == .online)
                        .padding(.bottom, button)
                }
            }
            .padding(.horizontal, Self.margin)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomLeading)
        }
        .animation(Motion.disclosure, value: voice.visible)
    }

    private func clearance(in frame: CGRect, across width: CGFloat) -> CGFloat {
        guard let dock = navigation.voiceDock, dock.minX < frame.minX + width, dock.minY < frame.maxY else { return 8 }
        return frame.maxY - dock.minY + 8
    }
}

struct ComposerMic: View {
    var into: String?

    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        if let target = navigation.voiceTarget(model) {
            VoiceButton(size: VoiceButton.inComposer, online: model.connection(target.computer)?.state == .online, into: into)
        }
    }
}

struct VoiceButton: View {
    static let floating: CGFloat = 50
    static let inComposer: CGFloat = 44
    static let hintFor = Duration.seconds(2)

    let size: CGFloat
    let online: Bool
    var into: String?

    @Environment(VoiceSession.self) private var voice
    @State private var hint = 0

    var body: some View {
        Button { if !enabled { hint += 1 } } label: {
            Image(systemName: enabled ? (voice.active ? "mic.fill" : "mic") : "mic.slash")
                .font(size < Self.floating ? .body.weight(.semibold) : .title3.weight(.semibold))
                .foregroundStyle(voice.active ? Color.onAccent : enabled ? Color.primary : Color.secondary)
                .frame(width: size, height: size)
                .background {
                    if voice.active {
                        Circle()
                            .fill(Color.accentColor)
                        Circle()
                            .stroke(Color.accentColor, lineWidth: 2)
                            .opacity(0.3 + 0.7 * voice.level)
                            .scaleEffect(1 + 0.2 * voice.level)
                            .animation(.easeOut(duration: 0.2), value: voice.level)
                    }
                }
                .glassEffect(voice.active ? .identity : .regular.interactive(), in: .circle)
                .contentShape(.circle)
        }
        .buttonStyle(VoicePressStyle(
            press: { if enabled { voice.press(at: ProcessInfo.processInfo.systemUptime, into: into) } },
            release: { if enabled { voice.release(at: ProcessInfo.processInfo.systemUptime) } }
        ))
        .accessibilityLabel(voice.active ? "Stop listening" : enabled ? label : "\(label), Mac offline")
        .accessibilityAction { if enabled { voice.active ? voice.finish() : voice.begin(into: into) } }
        .overlay(alignment: .bottomLeading) {
            if hint > 0 {
                Text("Mac offline")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .frame(minHeight: 32)
                    .glassEffect(.regular, in: .capsule)
                    .fixedSize()
                    .padding(.bottom, size + 8)
                    .transition(.opacity)
                    .accessibilityHidden(true)
            }
        }
        .task(id: hint) {
            guard hint > 0 else { return }
            try? await Task.sleep(for: Self.hintFor)
            withAnimation { hint = 0 }
        }
        .sensoryFeedback(.start, trigger: voice.active) { _, active in active }
        .sensoryFeedback(.warning, trigger: hint) { _, count in count > 0 }
    }

    private var label: String {
        into == nil ? "Talk to Clubmate" : "Dictate"
    }

    private var enabled: Bool {
        online || voice.active
    }
}

private struct VoiceDock: ViewModifier {
    @Environment(AppNavigation.self) private var navigation
    @State private var id = UUID()

    func body(content: Content) -> some View {
        content
            .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { frame in
                navigation.dock(id, at: frame)
            }
            .onDisappear {
                navigation.undock(id)
            }
    }
}

struct SearchBar: ViewModifier {
    let text: Binding<String>
    let prompt: String

    @Environment(AppNavigation.self) private var navigation
    @State private var searching = false

    func body(content: Content) -> some View {
        if navigation.layout == .compact {
            content
                .searchable(text: text, isPresented: $searching, prompt: prompt)
                .onChange(of: searching) { _, searching in navigation.searching = searching }
                .onAppear { navigation.searching = searching }
                .onDisappear { navigation.searching = false }
                .toolbar {
                    ToolbarItem(placement: .bottomBar) {
                        ComposerMic()
                            .voiceDock()
                    }
                    .sharedBackgroundVisibility(.hidden)
                    DefaultToolbarItem(kind: .search, placement: .bottomBar)
                }
        } else {
            content
                .searchable(text: text, prompt: prompt)
        }
    }
}

extension View {
    func voiceDock() -> some View {
        modifier(VoiceDock())
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

extension AppNavigation {
    func floatingMic(_ model: CompanionModel) -> Bool {
        !searching && !screenHasMic(model) && voiceTarget(model) != nil
    }

    private func screenHasMic(_ model: CompanionModel) -> Bool {
        guard let item = sidebar, let connection = model.connection(item.computer),
              let plugin = CompanionPlugins.all.first(where: { $0.id == item.plugin }) else { return false }
        if layout != .compact || preferredColumn == .detail {
            return detail.map { plugin.detailHasMic(connection: connection, selection: $0) } ?? false
        }
        return preferredColumn == .content && plugin.listHasMic(connection: connection)
    }

    func voiceTarget(_ model: CompanionModel) -> VoiceTarget? {
        let onSidebar = layout == .compact && preferredColumn == .sidebar
        let item = onSidebar ? nil : sidebar
        let single = model.computers.count == 1 ? model.computers.first?.id : nil
        guard let computer = item?.computer ?? (onSidebar ? nil : macDetails) ?? single,
              let connection = model.connection(computer), connection.voiceAvailable else { return nil }
        let workspace = workspace(for: connection)?.id
        guard workspace != nil || connection.state != .online else { return nil }
        let detailShown = layout != .compact || preferredColumn == .detail
        return .phone(computer: computer, workspace: workspace ?? "", plugin: item?.plugin, detail: item != nil && detailShown && !GraphRun.isRun(detail ?? "") ? detail : nil)
    }
}
