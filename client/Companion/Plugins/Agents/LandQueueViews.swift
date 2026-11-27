import SwiftUI

import CompanionKit

struct LandQueueSection: View {
    let queue: LandQueue
    let store: AgentsStore
    let open: (String) -> Void
    @State private var files: LandQueueEntry?
    @State private var removing: LandQueueEntry?

    var body: some View {
        Section {
            ForEach(queue.entries) { entry in
                row(entry)
            }
        } header: {
            HStack(spacing: 8) {
                Text(queue.title)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Spacer()
                Text(queue.detail)
                    .foregroundStyle(queue.detailTone.toneInk(default: Color.captionInk))
            }
        }
        .sheet(item: $files) { entry in
            LandQueueFilesSheet(entry: entry)
        }
        .alert(removing?.action("remove")?.confirm ?? "", isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }), presenting: removing) { entry in
            if let remove = entry.action("remove") {
                Button(remove.label, role: .destructive) { send(entry, remove) }
            }
            Button("Cancel", role: .cancel) {}
        }
    }

    private func row(_ entry: LandQueueEntry) -> some View {
        Button {
            if let session = entry.session { open(session) }
        } label: {
            LandQueueRow(entry: entry)
        }
        .buttonStyle(.plain)
        .listRowDensity()
        .tag("lq:" + queue.id + ":" + entry.id)
        .selectionDisabled()
        .columnRow()
        .contextMenu { menu(entry) }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) { swipes(entry) }
        .accessibilityIdentifier("queue." + entry.id)
    }

    private func menu(_ entry: LandQueueEntry) -> some View {
        Section(entry.line) {
            ForEach(entry.actions) { action in
                if action.divider == true { Divider() }
                Button(action.label, role: action.confirm == nil ? nil : .destructive) { run(entry, action) }
                    .disabled(!action.enabled)
            }
        }
    }

    @ViewBuilder
    private func swipes(_ entry: LandQueueEntry) -> some View {
        if let remove = entry.action("remove"), remove.enabled {
            Button(remove.label, role: .destructive) { run(entry, remove) }
        }
        if let skip = entry.action("skip"), skip.enabled {
            Button(skip.label) { run(entry, skip) }
                .tint(Color.gray)
        }
    }

    private func run(_ entry: LandQueueEntry, _ action: LandQueueAction) {
        guard action.enabled else { return }
        switch action.id {
        case "files": files = entry
        case "open": if let session = entry.session { open(session) }
        case "remove": removing = entry
        default: send(entry, action)
        }
    }

    private func send(_ entry: LandQueueEntry, _ action: LandQueueAction) {
        Task { try? await store.queueAct(queue, entry, action) }
    }
}

struct LandQueueRow: View {
    let entry: LandQueueEntry

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            LandQueueGlyph(glyph: entry.glyph, tone: entry.tone)
            VStack(alignment: .leading, spacing: 2) {
                Text(entry.title)
                    .font(.body.weight(entry.needsYou ? .semibold : .regular))
                    .lineLimit(2)
                if !entry.subtitle.isEmpty {
                    Text(entry.subtitle)
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Text(entry.word)
                .font(.subheadline)
                .foregroundStyle(entry.tone == "accent" ? Color.accentColor : entry.tone.toneInk(default: Color.captionInk))
                .fixedSize()
        }
        .padding(.leading, 24)
        .frame(minHeight: 44)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

struct LandQueueGlyph: View {
    let glyph: String
    let tone: String?

    var body: some View {
        Group {
            if let (ink, fill) = colours {
                Image(systemName: glyph.hasSuffix(".fill") ? glyph : glyph + ".fill")
                    .symbolRenderingMode(.palette)
                    .foregroundStyle(ink, fill)
            } else {
                Image(systemName: glyph)
                    .foregroundStyle(Color.captionInk)
            }
        }
        .font(.system(size: 24))
        .frame(width: 24, height: 24)
        .accessibilityHidden(true)
    }

    private var colours: (Color, Color)? {
        switch tone {
        case "warning": (Color.onWarning, Color.warning)
        case "accent": (Color.accentColor, Color.accentColor.opacity(0.18))
        case "danger": (Color.dangerInk, Color.dangerInk.opacity(0.18))
        case "success": (Color.successInk, Color.successInk.opacity(0.18))
        default: nil
        }
    }
}

struct LandQueueFilesSheet: View {
    let entry: LandQueueEntry
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                if let why = entry.why {
                    Section { Text(why).foregroundStyle(Color.captionInk) }
                }
                ForEach(entry.fileGroups) { group in
                    Section {
                        ForEach(group.files, id: \.self) { file in
                            Label {
                                Text(file)
                                    .font(.callout.monospaced())
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                            } icon: {
                                Image(systemName: group.shared == true ? "doc.on.doc" : "doc")
                                    .foregroundStyle(Color.captionInk)
                            }
                        }
                    } header: {
                        Text(group.title)
                    }
                }
                if let more = entry.more, more > 0 {
                    Text("+\(more) more").foregroundStyle(Color.captionInk)
                }
            }
            .navigationTitle(entry.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}
