import AppKit
import SwiftUI

import ClubmateKit

struct CommandPaletteView: View {
    let entries: [PaletteEntry]
    let run: (PaletteAction) -> Void
    let dismiss: () -> Void

    @State private var query = ""
    @State private var selection = 0
    @Environment(\.theme) private var theme

    private var ranked: [PaletteEntry] {
        Array(CommandPalette.rank(entries, query: query).prefix(12))
    }

    var body: some View {
        let rows = ranked
        VStack(spacing: 0) {
            PaletteField(text: $query, placeholder: "Type a command", move: move, submit: submit, cancel: dismiss)
                .frame(height: 30)
                .padding(.horizontal, theme.gutter)
            theme.border.frame(height: 1)
            VStack(spacing: 0) {
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, entry in
                    row(entry, selected: index == selection)
                        .onTapGesture { choose(entry) }
                }
                if rows.isEmpty {
                    Text("No matches")
                        .font(theme.ui)
                        .foregroundStyle(theme.muted)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, theme.gutter * 1.5)
                        .frame(height: theme.rowHeight + 2)
                }
            }
            .padding(.vertical, 4)
        }
        .frame(width: 480)
        .background(theme.background)
        .clipShape(RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.foreground.opacity(0.18), lineWidth: 1))
        .shadow(color: .black.opacity(0.35), radius: 16, y: 6)
        .onChange(of: query) { selection = 0 }
    }

    private func row(_ entry: PaletteEntry, selected: Bool) -> some View {
        HStack(spacing: theme.gutter) {
            Text(entry.title)
                .foregroundStyle(theme.foreground)
            Spacer(minLength: theme.gutter)
            Text(entry.detail)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
        }
        .font(theme.ui)
        .lineLimit(1)
        .padding(.horizontal, theme.gutter * 1.5)
        .frame(height: theme.rowHeight + 2)
        .background(selected ? theme.foreground.opacity(0.06) : .clear, ignoresSafeAreaEdges: [])
        .overlay(alignment: .leading) {
            if selected { theme.accent.frame(width: 2) }
        }
        .contentShape(Rectangle())
    }

    private func move(_ delta: Int) {
        let count = ranked.count
        guard count > 0 else { return }
        selection = (selection + delta + count) % count
    }

    private func submit() {
        let rows = ranked
        guard rows.indices.contains(selection) else { return }
        choose(rows[selection])
    }

    private func choose(_ entry: PaletteEntry) {
        dismiss()
        if let action = PaletteAction(entryID: entry.id) { run(action) }
    }
}

struct PaletteField: NSViewRepresentable {
    @Binding var text: String
    let placeholder: String
    let move: (Int) -> Void
    let submit: () -> Void
    let cancel: () -> Void
    var alternateSubmit: (() -> Void)?

    @Environment(\.theme) private var theme

    func makeCoordinator() -> Coordinator {
        Coordinator(self)
    }

    func makeNSView(context: Context) -> PaletteTextField {
        let field = PaletteTextField()
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.placeholderString = placeholder
        field.font = NSFont.monospacedSystemFont(ofSize: 13, weight: .regular)
        field.delegate = context.coordinator
        return field
    }

    func updateNSView(_ field: PaletteTextField, context: Context) {
        context.coordinator.parent = self
        field.textColor = NSColor(theme.foreground)
        if field.stringValue != text { field.stringValue = text }
    }

    @MainActor
    final class Coordinator: NSObject, NSTextFieldDelegate {
        var parent: PaletteField

        init(_ parent: PaletteField) {
            self.parent = parent
        }

        func controlTextDidChange(_ notification: Notification) {
            guard let field = notification.object as? NSTextField else { return }
            parent.text = field.stringValue
        }

        func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
            switch selector {
            case #selector(NSResponder.moveUp(_:)): parent.move(-1)
            case #selector(NSResponder.moveDown(_:)): parent.move(1)
            case #selector(NSResponder.insertNewline(_:)): parent.submit()
            case #selector(NSResponder.insertNewlineIgnoringFieldEditor(_:)):
                guard let alternate = parent.alternateSubmit else { return false }
                alternate()
            case #selector(NSResponder.cancelOperation(_:)): parent.cancel()
            default: return false
            }
            return true
        }
    }
}

final class PaletteTextField: NSTextField {
    private weak var previous: NSView?

    static func isShown(in window: NSWindow) -> Bool {
        owner(of: window.firstResponder) != nil
    }

    private static func owner(of responder: NSResponder?) -> PaletteTextField? {
        responder as? PaletteTextField ?? (responder as? NSTextView)?.delegate as? PaletteTextField
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window else { return }
        let responder = window.firstResponder
        if let owner = Self.owner(of: responder) {
            previous = owner.previous
        } else {
            previous = responder as? NSView
        }
        window.makeFirstResponder(self)
    }

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        if newWindow == nil, let window, Self.owner(of: window.firstResponder) === self, let previous, previous.window === window {
            window.makeFirstResponder(previous)
        }
        super.viewWillMove(toWindow: newWindow)
    }
}

extension View {
    func commandPalette(_ session: ProjectSession?, entries: @escaping () -> [PaletteEntry], run: @escaping (PaletteAction) -> Void) -> some View {
        overlay(alignment: .top) {
            if let session, session.overlay == .palette {
                CommandPaletteView(entries: entries(), run: run) { session.close(.palette) }
                    .padding(.top, 48)
            }
        }
    }
}
