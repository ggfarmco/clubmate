import SwiftUI

import ClubmateKit

struct FindInFilesView: View {
    let model: FindInFilesModel
    let dismiss: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        GeometryReader { geometry in
            panel
                .frame(width: geometry.size.width * 0.8, height: geometry.size.height * 0.7)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private var panel: some View {
        VStack(spacing: 0) {
            header
            theme.border.frame(height: 1)
            HStack(spacing: 0) {
                FindResultsList(model: model)
                    .frame(maxWidth: .infinity)
                theme.border.frame(width: 1)
                FindPreviewPane(model: model)
                    .frame(maxWidth: .infinity)
            }
            theme.border.frame(height: 1)
            footer
        }
        .background(theme.background)
        .clipShape(RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.foreground.opacity(0.18), lineWidth: 1))
        .shadow(color: .black.opacity(0.35), radius: 16, y: 6)
    }

    private var header: some View {
        HStack(spacing: theme.gutter) {
            PaletteField(
                text: Binding(get: { model.query }, set: { model.update(query: $0) }),
                placeholder: "Find in files",
                move: model.move,
                submit: open,
                cancel: dismiss,
                alternateSubmit: quickfix
            )
            .frame(height: 30)
            toggle("Aa", help: "Match Case", isOn: model.options.caseSensitive) { $0.caseSensitive.toggle() }
            toggle("ab|", help: "Match Whole Word", isOn: model.options.word) { $0.word.toggle() }
            toggle(".*", help: "Use Regular Expression", isOn: model.options.regex) { $0.regex.toggle() }
            PaletteField(
                text: Binding(get: { model.options.glob }, set: { glob in
                    var options = model.options
                    options.glob = glob
                    model.update(options: options)
                }),
                placeholder: "files to include, e.g. *.go",
                move: model.move,
                submit: open,
                cancel: dismiss,
                alternateSubmit: quickfix,
                autofocus: false,
                fontSize: 12
            )
            .frame(width: 200, height: 30)
        }
        .padding(.horizontal, theme.gutter)
    }

    private func toggle(_ label: String, help: String, isOn: Bool, change: @escaping (inout FindOptions) -> Void) -> some View {
        Button {
            var options = model.options
            change(&options)
            model.update(options: options)
        } label: {
            Text(label)
                .font(theme.uiSmall)
                .foregroundStyle(isOn ? theme.foreground : theme.muted)
                .frame(minWidth: 26, minHeight: 20)
                .background(isOn ? theme.accent.opacity(0.25) : .clear, in: RoundedRectangle(cornerRadius: 4))
        }
        .buttonStyle(.plain)
        .help(help)
    }

    private var footer: some View {
        HStack(spacing: theme.gutter) {
            Text(status)
                .foregroundStyle(theme.muted)
            Spacer(minLength: theme.gutter)
            Button("Quickfix ⌥↩", action: quickfix)
                .buttonStyle(.plain)
                .foregroundStyle(model.flat.isEmpty ? theme.muted : theme.accent)
                .disabled(model.flat.isEmpty)
        }
        .font(theme.uiSmall)
        .padding(.horizontal, theme.gutter * 1.5)
        .frame(height: theme.rowHeight + 4)
    }

    private var status: String {
        let count = "\(model.flat.count) results in \(model.groups.count) files"
        switch model.state {
        case .idle: return ""
        case .searching: return "\(count) — searching…"
        case .indexing: return "Indexing…"
        case .done: return count
        case .truncated: return "\(count) — stopped at 10 000"
        case .unavailable: return GoToFileModel.homeMessage
        case .error(let message): return message
        }
    }

    private func open() {
        Task { await model.open() }
    }

    private func quickfix() {
        Task { await model.quickfix() }
    }
}

private struct FindResultsList: View {
    let model: FindInFilesModel

    @Environment(\.theme) private var theme

    var body: some View {
        if let message = emptyMessage {
            Text(message)
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            let offsets = groupOffsets
            let selected = model.selected?.id
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(Array(model.groups.enumerated()), id: \.element.id) { groupIndex, group in
                            groupHeader(group)
                            ForEach(Array(group.matches.enumerated()), id: \.element.id) { index, match in
                                matchRow(match, index: offsets[groupIndex] + index, selected: match.id == selected)
                            }
                        }
                    }
                    .padding(.vertical, 4)
                }
                .onChange(of: selected) { _, id in
                    if let id { proxy.scrollTo(id) }
                }
            }
        }
    }

    private var groupOffsets: [Int] {
        var offsets: [Int] = []
        var total = 0
        for group in model.groups {
            offsets.append(total)
            total += group.matches.count
        }
        return offsets
    }

    private var emptyMessage: String? {
        guard model.flat.isEmpty else { return nil }
        switch model.state {
        case .unavailable: return GoToFileModel.homeMessage
        case .error(let message): return message
        case .idle: return "Type to search"
        case .searching: return "Searching…"
        case .indexing: return "Indexing…"
        case .done, .truncated: return "No results"
        }
    }

    private func groupHeader(_ group: FileGroup) -> some View {
        HStack(spacing: theme.gutter) {
            Text(group.path)
                .fontWeight(.semibold)
                .foregroundStyle(theme.foreground)
                .truncationMode(.head)
            Text("\(group.matches.count)")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
        }
        .font(theme.ui)
        .lineLimit(1)
        .padding(.horizontal, theme.gutter * 1.5)
        .frame(height: theme.rowHeight + 2)
    }

    private func matchRow(_ match: FindMatch, index: Int, selected: Bool) -> some View {
        HStack(spacing: theme.gutter) {
            Text("\(match.line)")
                .monospacedDigit()
                .foregroundStyle(theme.muted)
                .frame(width: 44, alignment: .trailing)
            Text(highlighted(match.runs))
        }
        .font(theme.ui)
        .lineLimit(1)
        .padding(.horizontal, theme.gutter)
        .frame(maxWidth: .infinity, minHeight: theme.rowHeight + 2, alignment: .leading)
        .background(selected ? theme.foreground.opacity(0.06) : .clear, ignoresSafeAreaEdges: [])
        .overlay(alignment: .leading) {
            if selected { theme.accent.frame(width: 2) }
        }
        .contentShape(Rectangle())
        .id(match.id)
        .onTapGesture(count: 2) {
            model.select(index)
            Task { await model.open() }
        }
        .onTapGesture { model.select(index) }
    }

    private func highlighted(_ runs: [HighlightRun]) -> AttributedString {
        runs.reduce(into: AttributedString()) { text, run in
            var part = AttributedString(run.text)
            part.foregroundColor = run.matched ? theme.accent : theme.foreground
            if run.matched { part.backgroundColor = theme.accent.opacity(0.15) }
            text += part
        }
    }
}

private struct FindPreviewPane: View {
    let model: FindInFilesModel

    @Environment(\.theme) private var theme

    var body: some View {
        if let preview = model.preview, let selected = model.selected, preview.matchID == selected.id {
            ScrollViewReader { proxy in
                ScrollView([.vertical, .horizontal]) {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(preview.lines, id: \.number) { line in
                            HStack(spacing: theme.gutter) {
                                Text("\(line.number)")
                                    .monospacedDigit()
                                    .foregroundStyle(theme.muted)
                                    .frame(width: 44, alignment: .trailing)
                                Text(line.text)
                                    .foregroundStyle(theme.foreground)
                                    .fixedSize()
                            }
                            .font(theme.ui)
                            .frame(maxWidth: .infinity, minHeight: theme.rowHeight, alignment: .leading)
                            .background(line.number == preview.line ? theme.accent.opacity(0.18) : .clear)
                            .id(line.number)
                        }
                    }
                    .padding(.vertical, 4)
                }
                .onChange(of: preview, initial: true) { _, preview in
                    proxy.scrollTo(preview.line, anchor: .center)
                }
            }
        } else if model.selected != nil {
            Text("Loading preview…")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            Color.clear
        }
    }
}

extension View {
    func findInFiles(_ session: ProjectSession?) -> some View {
        overlay {
            if let session, session.overlay == .findInFiles, let model = session.findInFiles {
                FindInFilesView(model: model) { session.close(.findInFiles) }
            }
        }
    }
}
