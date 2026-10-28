import AppKit
import SwiftUI

import ClubmateKit

struct SwitcherGridView: View {
    let windowID: String

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @State private var highlight: Int?

    static let tile = CGSize(width: 240, height: 150)
    private static let spacing: CGFloat = 16
    private static let padding: CGFloat = 24

    private var workspaces: [String] {
        app.window(windowID)?.workspaces ?? []
    }

    private var current: Int {
        let count = workspaces.count
        if let highlight { return min(highlight, count) }
        return app.window(windowID)?.active.flatMap(workspaces.firstIndex) ?? count
    }

    var body: some View {
        GeometryReader { proxy in
            let columns = columns(width: proxy.size.width)
            ZStack {
                Color.black.opacity(0.4)
                    .ignoresSafeArea()
                    .contentShape(Rectangle())
                    .onTapGesture { app.hideSwitcher() }
                ViewThatFits(in: .vertical) {
                    panel(columns: columns)
                    ScrollView { panel(columns: columns) }
                }
                .background(theme.background)
                .clipShape(RoundedRectangle(cornerRadius: 8))
                .overlay(RoundedRectangle(cornerRadius: 8).stroke(theme.foreground.opacity(0.18), lineWidth: 1))
                .shadow(color: .black.opacity(0.35), radius: 16, y: 6)
                .padding(Self.padding)
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel("Workspace switcher")
            .accessibilityAddTraits(.isModal)
            .background(SwitcherKeys { key in handle(key, columns: columns) })
        }
    }

    private func columns(width: CGFloat) -> Int {
        let available = width - Self.padding * 4 + Self.spacing
        return max(1, min(workspaces.count + 1, Int(available / (Self.tile.width + Self.spacing))))
    }

    private func panel(columns: Int) -> some View {
        let count = workspaces.count
        return VStack(alignment: .leading, spacing: Self.spacing) {
            ForEach(Array(stride(from: 0, through: count, by: columns)), id: \.self) { start in
                HStack(alignment: .top, spacing: Self.spacing) {
                    ForEach(start..<min(start + columns, count + 1), id: \.self) { index in
                        tile(index)
                    }
                }
            }
        }
        .padding(Self.padding)
    }

    private func tile(_ index: Int) -> some View {
        let workspace = workspaces.indices.contains(index) ? workspaces[index] : nil
        let highlighted = index == current
        return VStack(alignment: .leading, spacing: 6) {
            preview(workspace)
                .frame(width: Self.tile.width, height: Self.tile.height)
                .clipShape(RoundedRectangle(cornerRadius: 6))
                .overlay(
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(highlighted ? theme.accent : theme.border, lineWidth: highlighted ? 2 : 1)
                )
            HStack(spacing: 6) {
                if let workspace, workspace == app.window(windowID)?.active {
                    Circle().fill(theme.accent).frame(width: 6, height: 6)
                }
                Text(workspace.map { app.project($0)?.name ?? $0 } ?? "Open Folder…")
                    .font(theme.ui)
                    .foregroundStyle(highlighted ? theme.foreground : theme.muted)
                    .lineLimit(1)
            }
        }
        .frame(width: Self.tile.width, alignment: .leading)
        .contentShape(Rectangle())
        .onTapGesture { choose(index) }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(workspace.map { app.project($0)?.name ?? $0 } ?? "Open Folder…")
        .accessibilityAddTraits(workspace != nil && workspace == app.window(windowID)?.active ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction { choose(index) }
    }

    @ViewBuilder private func preview(_ workspace: String?) -> some View {
        if let workspace {
            let session = app.existingSession(for: workspace)
            if let image = session?.thumbnail {
                Image(nsImage: image)
                    .resizable()
                    .aspectRatio(contentMode: .fill)
                    .frame(width: Self.tile.width, height: Self.tile.height, alignment: .top)
            } else {
                placeholder(section(session))
            }
        } else {
            Image(systemName: "plus")
                .font(.system(size: 28, weight: .light))
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(theme.foreground.opacity(0.03))
        }
    }

    private func section(_ session: ProjectSession?) -> SectionInfo? {
        guard let store = session?.store else { return nil }
        return store.sections.first { $0.id == store.selectedSection }
    }

    private func placeholder(_ section: SectionInfo?) -> some View {
        VStack(spacing: theme.gutter) {
            Image(systemName: SymbolName.resolve(section?.icon ?? "", fallback: "square.grid.2x2"))
                .font(.system(size: 28, weight: .light))
            if let section {
                Text(section.title).font(theme.uiSmall)
            }
        }
        .foregroundStyle(theme.muted)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(theme.foreground.opacity(0.03))
    }

    private func handle(_ key: SwitcherKey, columns: Int) {
        switch key {
        case .arrow(let arrow):
            highlight = SwitcherGrid.move(highlight: current, count: workspaces.count, columns: columns, key: arrow)
        case .choose:
            choose(current)
        case .dismiss:
            app.hideSwitcher()
        }
    }

    private func choose(_ index: Int) {
        guard workspaces.indices.contains(index) else {
            app.hideSwitcher()
            return app.openFolder(in: windowID)
        }
        app.activate(workspaces[index], in: windowID)
        app.hideSwitcher()
    }
}

enum SwitcherKey {
    case arrow(SwitcherGrid.Arrow)
    case choose
    case dismiss
}

private struct SwitcherKeys: NSViewRepresentable {
    let handle: (SwitcherKey) -> Void

    func makeNSView(context: Context) -> SwitcherKeyView {
        let view = SwitcherKeyView()
        view.handle = handle
        return view
    }

    func updateNSView(_ view: SwitcherKeyView, context: Context) {
        view.handle = handle
    }
}

final class SwitcherKeyView: NSView {
    var handle: ((SwitcherKey) -> Void)?
    private weak var previous: NSView?

    override var acceptsFirstResponder: Bool { true }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window else { return }
        previous = window.firstResponder as? NSView
        window.makeFirstResponder(self)
    }

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        if newWindow == nil, let window, window.firstResponder === self {
            window.makeFirstResponder(previous?.window === window ? previous : nil)
        }
        super.viewWillMove(toWindow: newWindow)
    }

    override func keyDown(with event: NSEvent) {
        interpretKeyEvents([event])
    }

    override func doCommand(by selector: Selector) {
        switch selector {
        case #selector(NSResponder.moveLeft(_:)): handle?(.arrow(.left))
        case #selector(NSResponder.moveRight(_:)): handle?(.arrow(.right))
        case #selector(NSResponder.moveUp(_:)): handle?(.arrow(.up))
        case #selector(NSResponder.moveDown(_:)): handle?(.arrow(.down))
        case #selector(NSResponder.insertNewline(_:)): handle?(.choose)
        case #selector(NSResponder.cancelOperation(_:)): handle?(.dismiss)
        default: break
        }
    }
}

@MainActor
enum WorkspaceThumbnail {
    static func capture(_ window: NSWindow) -> NSImage? {
        guard let root = window.contentView, root.bounds.width > 0, root.bounds.height > 0,
              let screen = root.bitmapImageRepForCachingDisplay(in: root.bounds) else { return nil }
        root.cacheDisplay(in: root.bounds, to: screen)
        let tile = SwitcherGridView.tile
        let scale = max(tile.width / root.bounds.width, tile.height / root.bounds.height)
        let size = NSSize(width: (root.bounds.width * scale).rounded(), height: (root.bounds.height * scale).rounded())
        let backing = window.backingScaleFactor
        guard let rep = NSBitmapImageRep(
            bitmapDataPlanes: nil, pixelsWide: Int(size.width * backing), pixelsHigh: Int(size.height * backing),
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0
        ) else { return nil }
        rep.size = size
        guard let context = NSGraphicsContext(bitmapImageRep: rep) else { return nil }
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = context
        screen.draw(in: NSRect(origin: .zero, size: size))
        NSGraphicsContext.restoreGraphicsState()
        let image = NSImage(size: size)
        image.addRepresentation(rep)
        return image
    }
}
