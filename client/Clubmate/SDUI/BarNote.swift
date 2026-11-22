import SwiftUI

import ClubmateKit

struct BarNote {
    let text: String
    let action: String
    let dismiss: @MainActor () -> Void
}

enum SDUIClientAction {
    static let hideChat = "hideChat"
}

extension EnvironmentValues {
    @Entry var barNote: BarNote?
    @Entry var railKeys: [String: String] = [:]
    @Entry var sduiClientAction: (@MainActor (String) -> Bool)?
}

struct BarNoteView: View {
    let note: BarNote

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.l) {
            Text(note.text)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(note.action) { note.dismiss() }
                .buttonStyle(.plain)
                .font(theme.uiSmall)
                .foregroundStyle(theme.text2)
                .padding(theme.spacing.badge)
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(hovering ? theme.hover : .clear))
                .onHover { hovering = $0 }
        }
        .padding(.leading, theme.spacing.xl)
        .padding(.trailing, theme.spacing.m)
        .padding(.bottom, theme.spacing.m)
    }

    @State private var hovering = false
}

struct KeyCapLabel: View {
    let label: String
    var onAccent = false

    @Environment(\.theme) private var theme

    var body: some View {
        Text(label)
            .font(theme.uiSmall)
            .foregroundStyle(onAccent ? theme.onAccent : theme.muted)
            .padding(theme.spacing.badge)
            .overlay(RoundedRectangle(cornerRadius: theme.radius.s).strokeBorder(onAccent ? theme.onAccent.opacity(0.6) : theme.borderStrong))
            .fixedSize()
    }
}
