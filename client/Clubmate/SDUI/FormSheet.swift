import AppKit
import SwiftUI

import ClubmateKit

struct FormSheetHost: View {
    static let width: CGFloat = 560
    static let cancelAction = "cancel"
    static let cancelHint = "⎋"
    static let windowID = NSUserInterfaceItemIdentifier("clubmate.formSheet")

    let node: SDUINode

    @State private var shown = true
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.composerDrafts) private var drafts
    @Environment(\.theme) private var theme

    var body: some View {
        Color.clear
            .frame(width: 0, height: 0)
            .sheet(isPresented: $shown, onDismiss: cancel) {
                FormNodeView(node: node)
                    .padding(.horizontal, theme.spacing.xl)
                    .padding(.vertical, theme.spacing.m)
                    .frame(width: Self.width)
                    .background(theme.surface3)
                    .onExitCommand { shown = false }
                    .environment(\.sduiDispatch, dispatch)
                    .environment(\.composerDrafts, drafts)
                    .environment(\.theme, theme)
                    .environment(\.formSheet, true)
                    .background(FormSheetWindowTag())
            }
    }

    private func cancel() {
        dispatch(node, .named(Self.cancelAction, .null))
    }
}

private struct FormSheetKey: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    var formSheet: Bool {
        get { self[FormSheetKey.self] }
        set { self[FormSheetKey.self] = newValue }
    }
}

private struct FormSheetWindowTag: NSViewRepresentable {
    func makeNSView(context: Context) -> FormSheetWindowTagView {
        FormSheetWindowTagView()
    }

    func updateNSView(_ view: FormSheetWindowTagView, context: Context) {}
}

private final class FormSheetWindowTagView: NSView {
    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        window?.identifier = FormSheetHost.windowID
    }
}
