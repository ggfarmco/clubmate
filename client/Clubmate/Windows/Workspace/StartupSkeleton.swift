import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class StartupHandOver {
    static let fade = 0.18
    static let fastPath: Duration = .milliseconds(250)
    static let group: Duration = .milliseconds(400)

    private(set) var skeleton: Bool
    @ObservationIgnored private let shown = ContinuousClock.now

    init(skeleton: Bool) {
        self.skeleton = skeleton
    }

    func finish(reduceMotion: Bool) {
        guard skeleton else { return }
        guard let animation = Self.animation(elapsed: ContinuousClock.now - shown, reduceMotion: reduceMotion) else {
            skeleton = false
            return
        }
        withAnimation(animation) { skeleton = false }
    }

    static func animation(elapsed: Duration, reduceMotion: Bool) -> Animation? {
        reduceMotion || elapsed <= fastPath ? nil : .easeOut(duration: fade)
    }
}

struct StartupSkeleton: View {
    let record: LaunchRecord?
    let selected: String?
    let note: String?
    let showsLog: Bool
    let failure: String?
    let select: (String) -> Void

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @State private var width: CGFloat = 0
    @State private var announced = false

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                SkeletonRail(sections: record.map { app.settings.ordered($0.sections) }, selected: section, select: select)
                theme.border.frame(width: 1)
                if sidebar, app.settings.sidebarSide == .left {
                    sidebarPane
                    theme.border.frame(width: 1)
                }
                content {
                    mainPane
                    if sidebar, app.settings.sidebarSide == .right {
                        theme.border.frame(width: 1)
                        sidebarPane
                    }
                    if chat {
                        if stacked { theme.border.frame(height: 1) } else { theme.border.frame(width: 1) }
                        PaneSkeleton(kind: failure == nil ? kind(CodeSection.chatSlot) : .terminal, slot: CodeSection.chatSlot)
                            .frame(width: stacked ? nil : chatWidth)
                            .frame(maxHeight: .infinity)
                    }
                }
            }
            .frame(maxHeight: .infinity)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
            SkeletonStatusBar(note: note, showsLog: showsLog, failed: failure != nil)
        }
        .background(theme.background, ignoresSafeAreaEdges: [])
        .accessibilityElement(children: failure == nil ? .ignore : .contain)
        .accessibilityLabel(failure == nil ? "Starting core" : "")
        .onChange(of: note) { old, new in
            guard old == nil, let new, !announced else { return }
            announced = true
            AccessibilityNotification.Announcement(new).post()
        }
    }

    @ViewBuilder private var mainPane: some View {
        if let failure {
            StartupFailure(message: failure)
                .padding(theme.spacing.xxl)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        } else {
            PaneSkeleton(kind: kind(main), slot: main)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private var section: String? {
        selected ?? record?.selected
    }

    private var main: String {
        "\(section ?? "").main"
    }

    private var sidebar: Bool {
        guard let record, let section else { return true }
        return record.sidebarShown(section) && record.kinds["\(section).sidebar"] != nil
    }

    private var chat: Bool {
        guard let record, let section else { return false }
        return record.chatShown(in: section)
    }

    private var stacked: Bool {
        CodeSection.stacksChat(width: Double(width))
    }

    private var content: AnyLayout {
        chat && stacked ? AnyLayout(VStackLayout(spacing: 0)) : AnyLayout(HStackLayout(spacing: 0))
    }

    private var chatWidth: CGFloat {
        CGFloat(CodeSection.columnWidth(saved: record?.chatWidth, limit: width > 0 ? Double(width) / 2 : 640))
    }

    private var sidebarPane: some View {
        let slot = "\(section ?? "").sidebar"
        return PaneSkeleton(kind: failure != nil ? .terminal : kind(slot) == .unknown ? .list : kind(slot), slot: slot)
            .frame(width: min(max(CGFloat(record?.sidebarWidth ?? 248), 200), 320))
            .frame(maxHeight: .infinity)
            .background(theme.surface1, ignoresSafeAreaEdges: [])
    }

    private func kind(_ slot: String) -> PaneKind {
        record?.kind(slot) ?? .unknown
    }
}

private struct SkeletonRail: View {
    let sections: [LaunchSection]?
    let selected: String?
    let select: (String) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            if let sections, !sections.isEmpty {
                ForEach(sections, id: \.id) { section in
                    Button { select(section.id) } label: {
                        Image(systemName: SymbolName.resolve(section.icon, fallback: "square.grid.2x2"))
                            .font(.system(size: 14))
                            .foregroundStyle(theme.faint)
                            .frame(width: theme.rail, height: 36)
                            .background(section.id == selected ? theme.hover : .clear)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help(section.title)
                }
            } else {
                ForEach(0..<6, id: \.self) { _ in
                    RoundedRectangle(cornerRadius: 4)
                        .fill(theme.pressed)
                        .frame(width: 16, height: 16)
                        .frame(width: theme.rail, height: 36)
                }
            }
            Spacer(minLength: 0)
        }
        .frame(width: theme.rail)
        .frame(maxHeight: .infinity)
        .background(theme.surface1, ignoresSafeAreaEdges: [])
    }
}

private struct SkeletonStatusBar: View {
    let note: String?
    let showsLog: Bool
    let failed: Bool

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.l) {
            if failed {
                HStack(spacing: theme.spacing.s) {
                    Text("●")
                    Text("core didn't start")
                }
                .font(theme.uiSmall)
                .foregroundStyle(theme.danger)
                Spacer(minLength: 0)
            } else if let note {
                StartupNote(text: note, showsLog: showsLog)
                Spacer(minLength: 0)
            } else {
                SkeletonBar(width: 56)
                Spacer(minLength: 0)
                SkeletonBar(width: 72)
                SkeletonBar(width: 40)
            }
        }
        .padding(.horizontal, theme.gutter)
        .frame(height: 22)
        .background(theme.surface1, ignoresSafeAreaEdges: [])
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
    }
}

struct StartupNote: View {
    let text: String
    let showsLog: Bool

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            if !reduceMotion {
                ProgressView()
                    .controlSize(.mini)
            }
            Text(text)
                .foregroundStyle(theme.muted)
            if showsLog {
                Button { app.showLog() } label: {
                    HStack(spacing: theme.spacing.s) {
                        Text("Show log")
                        Text(Shortcut.label(AppShortcuts.showLog.key)).foregroundStyle(theme.muted)
                    }
                }
                .buttonStyle(.plain)
                .foregroundStyle(theme.accentInk)
                .keyboardShortcut(AppShortcuts.showLog)
            }
        }
        .font(theme.uiSmall)
        .lineLimit(1)
    }
}

struct StartupFailure: View {
    let message: String

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xl) {
            Text(message)
                .font(theme.ui)
                .foregroundStyle(theme.danger)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: theme.spacing.l) {
                CardButton(label: "Retry", shortcut: Shortcut.label(AppShortcuts.retry.key), tone: .primary) { app.retry() }
                CardButton(label: "Show log", shortcut: Shortcut.label(AppShortcuts.showLog.key)) { app.showLog() }
                    .keyboardShortcut(AppShortcuts.showLog)
            }
        }
    }
}

struct PaneSkeleton: View {
    let kind: PaneKind
    let slot: String

    @Environment(\.theme) private var theme

    var body: some View {
        Group {
            switch kind {
            case .list, .unknown: list
            case .transcript: transcript
            case .detail: detail
            case .table: table
            case .terminal: Color.clear
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .clipped()
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private var list: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: theme.spacing.m) {
                SkeletonBar(width: 72, title: true)
                Spacer(minLength: 0)
                SkeletonIcon()
                SkeletonIcon()
            }
            .frame(height: theme.header)
            .padding(.horizontal, theme.gutter)
            if slot == AgentsSection.sidebarSlot {
                HStack(spacing: theme.spacing.s) {
                    SkeletonChip(width: 40)
                    SkeletonChip(width: 52)
                    SkeletonChip(width: 44)
                }
                .padding(.horizontal, theme.gutter)
                .padding(.bottom, theme.spacing.l)
            }
            SkeletonBar(width: 48)
                .frame(height: theme.rowHeight)
                .padding(.horizontal, theme.gutter)
            ForEach(Array(Self.rows.enumerated()), id: \.offset) { _, row in
                HStack(spacing: theme.spacing.m) {
                    SkeletonIcon()
                    SkeletonBar(width: row.width)
                }
                .frame(height: theme.rowHeight)
                .padding(.leading, theme.gutter + CGFloat(row.depth) * theme.indent)
            }
        }
    }

    private var transcript: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xl) {
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                SkeletonBar(width: 220, title: true)
                SkeletonBar(width: 120)
            }
            .padding(.vertical, theme.spacing.l)
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                SkeletonBar(width: 300)
                SkeletonBar(width: 180)
            }
            .padding(theme.spacing.xl)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface1))
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                ForEach([96, 72, 112, 84], id: \.self) { width in
                    HStack(spacing: theme.spacing.m) {
                        SkeletonIcon()
                        SkeletonBar(width: CGFloat(width))
                    }
                }
            }
            .padding(theme.spacing.xl)
            .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface2))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.border))
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                ForEach([230, 140, 180], id: \.self) { width in
                    HStack(spacing: theme.spacing.m) {
                        SkeletonIcon()
                        SkeletonBar(width: CGFloat(width))
                    }
                }
            }
            VStack(alignment: .leading, spacing: theme.spacing.m) {
                SkeletonBar(width: 330)
                SkeletonBar(width: 310)
                SkeletonBar(width: 140)
            }
            Spacer(minLength: 0)
            RoundedRectangle(cornerRadius: theme.radius.l)
                .fill(theme.surface2)
                .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.border))
                .frame(height: 56)
                .padding(.bottom, Self.composerControls)
        }
        .padding(theme.spacing.xxl)
    }

    private var detail: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xl) {
            HStack(spacing: theme.spacing.m) {
                SkeletonIcon()
                SkeletonBar(width: 180, title: true)
            }
            VStack(alignment: .leading, spacing: theme.spacing.l) {
                ForEach([140, 100, 120], id: \.self) { width in
                    HStack(spacing: theme.spacing.xl) {
                        SkeletonBar(width: 64)
                        SkeletonBar(width: CGFloat(width))
                    }
                }
            }
            SkeletonBar(width: 56)
            RoundedRectangle(cornerRadius: theme.radius.l)
                .fill(theme.surface1)
                .frame(height: 80)
        }
        .padding(theme.spacing.xxl)
    }

    private var table: some View {
        VStack(alignment: .leading, spacing: 0) {
            tableRow.opacity(0.7)
            ForEach(0..<10, id: \.self) { _ in tableRow }
        }
        .padding(.horizontal, theme.gutter)
    }

    private var tableRow: some View {
        HStack(spacing: theme.spacing.xl) {
            ForEach(Array(Self.columns.enumerated()), id: \.offset) { _, width in
                SkeletonBar(width: width)
            }
        }
        .frame(height: theme.rowHeight)
    }

    private static let rows: [(depth: Int, width: CGFloat)] = [(0, 120), (1, 96), (1, 140), (1, 110), (0, 88), (1, 132), (1, 104), (0, 116), (1, 92)]
    private static let columns: [CGFloat] = [140, 64, 80, 56, 96]
    private static let composerControls: CGFloat = 21
}

struct SkeletonBar: View {
    let width: CGFloat
    var title = false

    @Environment(\.theme) private var theme

    var body: some View {
        RoundedRectangle(cornerRadius: title ? 5 : 4)
            .fill(theme.pressed)
            .frame(width: width, height: title ? 10 : 8)
    }
}

private struct SkeletonIcon: View {
    @Environment(\.theme) private var theme

    var body: some View {
        RoundedRectangle(cornerRadius: 3)
            .fill(theme.pressed)
            .frame(width: 12, height: 12)
    }
}

private struct SkeletonChip: View {
    let width: CGFloat

    @Environment(\.theme) private var theme

    var body: some View {
        RoundedRectangle(cornerRadius: 8)
            .fill(theme.pressed)
            .frame(width: width, height: 16)
    }
}
