import SwiftUI

import ClubmateKit

struct GoToFileView: View {
    let dismiss: () -> Void

    @State private var model: GoToFileModel
    @Environment(\.theme) private var theme

    private static let visibleRows = 14

    init(session: ProjectSession) {
        dismiss = { [weak session] in session?.close(.goToFile) }
        _model = State(initialValue: GoToFileModel(
            run: { [weak session] invocation in
                guard let store = session?.store else { return .failure(.notRunning) }
                return await store.run(invocation)
            },
            dismiss: { [weak session] in session?.close(.goToFile) },
            report: { [weak session] message in session?.banner.show(message) },
            opened: { [weak session] in session?.store?.requestFocus(section: CodeSection.id, slot: CodeSection.mainSlot) },
            pick: session.fileTarget
        ))
    }

    var body: some View {
        VStack(spacing: 0) {
            PaletteField(
                text: Binding(get: { model.query }, set: { model.update($0) }),
                placeholder: model.picking ? "Attach file" : "Go to file (path:line)",
                move: model.move,
                submit: choose,
                cancel: dismiss
            )
            .frame(height: 30)
            .padding(.horizontal, theme.gutter)
            theme.border.frame(height: 1)
            results
                .padding(.vertical, 4)
            if let footer {
                theme.border.frame(height: 1)
                Text(footer)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, theme.gutter * 1.5)
                    .frame(height: theme.rowHeight)
            }
        }
        .frame(width: 560)
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
                        ForEach(Array(model.results.enumerated()), id: \.element.id) { index, match in
                            row(match, selected: index == model.selection)
                                .id(match.id)
                                .onTapGesture {
                                    model.select(index)
                                    choose()
                                }
                        }
                    }
                }
                .frame(height: (theme.rowHeight + 2) * CGFloat(min(model.results.count, Self.visibleRows)))
                .onChange(of: model.selection) { _, selection in
                    if model.results.indices.contains(selection) { proxy.scrollTo(model.results[selection].id) }
                }
            }
        }
    }

    private var footer: String? {
        guard model.status == .ready else { return nil }
        if model.capped { return "Index capped at 200 000 files" }
        return model.truncated ? "More results — keep typing" : nil
    }

    private var message: String? {
        switch model.status {
        case .indexing: "Indexing…"
        case .unavailable: GoToFileModel.homeMessage
        case .failed(let error): error
        case .ready: model.results.isEmpty ? (model.query.isEmpty ? "No files" : "No matches") : nil
        }
    }

    private func row(_ match: GoToFileMatch, selected: Bool) -> some View {
        let start = match.nameStart
        let end = match.path.unicodeScalars.count
        return HStack(spacing: theme.gutter) {
            Text(highlighted(match.runs(in: start..<end), base: theme.foreground))
                .fontWeight(.semibold)
            Text(highlighted(match.runs(in: 0..<max(start - 1, 0)), base: theme.muted))
                .truncationMode(.path)
            Spacer(minLength: theme.gutter)
            if let line = match.line {
                Text(":\(line)")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
            }
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

    private func highlighted(_ runs: [HighlightRun], base: Color) -> AttributedString {
        runs.reduce(into: AttributedString()) { text, run in
            var part = AttributedString(run.text)
            part.foregroundColor = run.matched ? theme.accent : base
            text += part
        }
    }

    private func choose() {
        Task { await model.choose() }
    }
}

extension View {
    func goToFile(_ session: ProjectSession?) -> some View {
        overlay(alignment: .top) {
            if let session, session.overlay == .goToFile {
                GoToFileView(session: session)
                    .id(session.attachRequest)
                    .padding(.top, 48)
            }
        }
    }
}
