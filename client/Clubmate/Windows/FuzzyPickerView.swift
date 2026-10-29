import SwiftUI

import ClubmateKit

struct FuzzyPickerView: View {
    let model: FuzzyPickerModel
    let choose: (PickerItem) -> Void
    let dismiss: () -> Void

    @Environment(\.theme) private var theme

    private static let visibleRows = 12

    var body: some View {
        VStack(spacing: 0) {
            PaletteField(
                text: Binding(get: { model.query }, set: { model.update($0) }),
                placeholder: model.placeholder,
                move: model.move,
                submit: submit,
                cancel: dismiss
            )
            .frame(height: 30)
            .padding(.horizontal, theme.gutter)
            theme.border.frame(height: 1)
            results
                .padding(.vertical, 4)
        }
        .frame(width: 480)
        .background(theme.background)
        .clipShape(RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.foreground.opacity(0.18), lineWidth: 1))
        .shadow(color: .black.opacity(0.35), radius: 16, y: 6)
        .onAppear { model.update("") }
        .onDisappear { model.cancel() }
    }

    @ViewBuilder private var results: some View {
        if let message {
            Text(message)
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, theme.gutter * 1.5)
                .frame(height: theme.rowHeight + 2)
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(Array(model.items.enumerated()), id: \.element.id) { index, item in
                            row(item, selected: index == model.selection)
                                .id(item.id)
                                .onTapGesture {
                                    model.select(index)
                                    submit()
                                }
                        }
                    }
                }
                .frame(height: (theme.rowHeight + 2) * CGFloat(min(model.items.count, Self.visibleRows)))
                .onChange(of: model.selection) { _, selection in
                    if model.items.indices.contains(selection) { proxy.scrollTo(model.items[selection].id) }
                }
            }
        }
    }

    private var message: String? {
        if let failure = model.failure { return failure }
        guard model.items.isEmpty else { return nil }
        return model.loaded ? "No matches" : "Loading…"
    }

    private func row(_ item: PickerItem, selected: Bool) -> some View {
        HStack(spacing: theme.gutter) {
            Text(item.label)
                .foregroundStyle(theme.foreground)
                .truncationMode(.middle)
            Spacer(minLength: theme.gutter)
            Text(item.detail)
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

    private func submit() {
        Task {
            guard let item = await model.choose() else { return }
            dismiss()
            choose(item)
        }
    }
}

extension View {
    func fuzzyPicker(_ session: ProjectSession?) -> some View {
        overlay(alignment: .top) {
            if let session, session.overlay == .picker, let request = session.picker {
                FuzzyPickerView(model: request.model, choose: request.choose) { session.close(.picker) }
                    .id(ObjectIdentifier(request.model))
                    .padding(.top, 48)
            }
        }
    }
}
