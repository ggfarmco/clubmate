import AppKit
import Foundation
import Observation
import QuickLook
import SwiftUI

import ClubmateKit

extension AppModel {
    func report(_ request: ReportRequest, _ expiry: ConfirmExpiry) async -> ReportReply {
        guard await reports.turn(expiry) else { return .unanswered(request, reason: ReportReply.timedOut) }
        defer { reports.done() }
        guard !expiry.expired else { return .unanswered(request, reason: ReportReply.timedOut) }
        guard let window = windows.reportWindow(request.workspaceId) else { return .unanswered(request, reason: ReportReply.noWindow) }
        let attention = TestMode.current ? nil : NSApp.requestUserAttention(.criticalRequest)
        defer { attention.map(NSApp.cancelUserAttentionRequest) }
        let host = windows.host(window)
        let extra = Diagnostics.appExtras(window: host, screenshot: true)
        let reply = await reports.present(request, extra: extra, in: window, host: host, expiry: expiry)
        if expiry.expired {
            let message = "The problem report waited too long and closed. Nothing was sent."
            if let banner = sessions.existing(request.workspaceId)?.banner { banner.show(message) } else { AppAlert.warning(message) }
        }
        return reply
    }
}

@MainActor
@Observable
final class ReportModel {
    struct Item: Identifiable, Equatable {
        let name: String
        let bytes: Int
        let path: String
        let warning: String
        let source: String
        let agent: Bool
        let url: URL?
        var checked: Bool

        var id: String { name }
    }

    private(set) var request: ReportRequest?
    private(set) var window: String?
    var text = ""
    var summary = ""
    var items: [Item] = []
    var preview: URL?

    @ObservationIgnored private var extra: [String: Data] = [:]
    @ObservationIgnored private var scratch: URL?
    @ObservationIgnored private var answer: CheckedContinuation<ReportReply, Never>?
    @ObservationIgnored private var closing: (any NSObjectProtocol)?
    @ObservationIgnored private var busy = false
    @ObservationIgnored private var waiting: [(id: UUID, turn: CheckedContinuation<Bool, Never>)] = []

    func turn(_ expiry: ConfirmExpiry) async -> Bool {
        guard busy else {
            busy = true
            return true
        }
        let id = UUID()
        return await withCheckedContinuation { continuation in
            waiting.append((id, continuation))
            expiry.onExpire { [weak self] in self?.leave(id) }
        }
    }

    func done() {
        if waiting.isEmpty { busy = false } else { waiting.removeFirst().turn.resume(returning: true) }
    }

    private func leave(_ id: UUID) {
        guard let index = waiting.firstIndex(where: { $0.id == id }) else { return }
        waiting.remove(at: index).turn.resume(returning: false)
    }

    func present(_ request: ReportRequest, extra: [String: Data], in window: String, host: NSWindow?, expiry: ConfirmExpiry) async -> ReportReply {
        let scratch = FileManager.default.temporaryDirectory.appending(path: "clubmate-report-\(UUID().uuidString)")
        let files = request.files.map { file in
            Item(name: file.name, bytes: file.bytes, path: file.path, warning: file.warning, source: file.source.isEmpty ? file.path : file.source, agent: !file.source.isEmpty || file.name.hasPrefix("files/"), url: file.path.isEmpty ? nil : URL(filePath: file.path), checked: file.checked)
        }
        let extras = [Diagnostics.settingsName, Diagnostics.appLogName, Diagnostics.windowName].compactMap { name -> Item? in
            guard let data = extra[name] else { return nil }
            let url = scratch.appending(path: name)
            let written = (try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)).flatMap { try? data.write(to: url) } != nil
            return Item(name: name, bytes: data.count, path: "", warning: "", source: "", agent: false, url: written ? url : nil, checked: ReportRequest.extraChecked[name] ?? false)
        }
        self.request = request
        self.window = window
        self.extra = extra
        self.scratch = scratch
        text = request.text
        summary = request.summary
        items = files.filter(\.agent) + files.filter { !$0.agent } + extras
        closing = host.map { host in
            NotificationCenter.default.addObserver(forName: NSWindow.willCloseNotification, object: host, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.cancel() }
            }
        }
        return await withCheckedContinuation { continuation in
            answer = continuation
            expiry.onExpire { [weak self] in self?.finish(.unanswered(request, reason: ReportReply.timedOut)) }
        }
    }

    func presented(in window: String?) -> Bool {
        request != nil && window != nil && self.window == window
    }

    var total: Int {
        items.filter(\.checked).reduce(0) { $0 + $1.bytes }
    }

    var blocked: String? {
        request?.textProblem(text: text, summary: summary) ?? request?.overLimit(total)
    }

    func send() {
        guard blocked == nil else { return }
        let kept = Set(items.filter(\.checked).map(\.name))
        finish(ReportReply(send: true, text: text, summary: summary, files: items.filter { $0.checked && extra[$0.name] == nil }.map(\.name), extra: extra.filter { kept.contains($0.key) }, appVersion: Bundle.main.clubmateVersion))
    }

    func cancel() {
        guard let request else { return }
        finish(.cancelled(request))
    }

    private func finish(_ reply: ReportReply) {
        guard let answer else { return }
        self.answer = nil
        closing.map(NotificationCenter.default.removeObserver)
        closing = nil
        request = nil
        window = nil
        preview = nil
        extra = [:]
        if let scratch { try? FileManager.default.removeItem(at: scratch) }
        scratch = nil
        answer.resume(returning: reply)
    }
}

struct ReportSheet: View {
    static let width: CGFloat = 560
    static let visibleRows = 8
    static let windowID = NSUserInterfaceItemIdentifier("clubmate.reportSheet")

    @Bindable var model: ReportModel

    @FocusState private var focus: Field?
    @Environment(\.theme) private var theme

    private enum Field {
        case text
        case summary
    }

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            VStack(alignment: .leading, spacing: theme.spacing.xs) {
                Text("Send a problem report")
                    .font(theme.heading)
                    .foregroundStyle(theme.foreground)
                Text("Goes to \(model.request?.relayHost ?? "")")
                    .font(theme.ui)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            editor("Your words", text: $model.text, field: .text)
            editor("Agent's summary", text: $model.summary, field: .summary)
            files
            footer
        }
        .padding(theme.spacing.xxl)
        .frame(width: Self.width, alignment: .topLeading)
        .background(theme.surface3)
        .onExitCommand { model.cancel() }
        .quickLookPreview($model.preview)
        .background(SheetWindowTag(id: Self.windowID))
    }

    private func editor(_ label: String, text: Binding<String>, field: Field) -> some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            Text(label)
                .font(theme.uiSmall)
                .foregroundStyle(theme.text2)
            TextEditor(text: text)
                .font(theme.ui)
                .foregroundStyle(theme.foreground)
                .scrollContentBackground(.hidden)
                .focused($focus, equals: field)
                .padding(.horizontal, theme.spacing.s)
                .padding(.vertical, theme.spacing.s)
                .frame(height: theme.rowHeight * 3)
                .fieldChrome(focused: focus == field)
                .testNode(field == .text ? "report.text" : "report.summary", role: "editor")
        }
    }

    private var files: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            HStack(spacing: theme.spacing.s) {
                Text("Files")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                Spacer(minLength: theme.spacing.s)
                Text("\(model.items.filter(\.checked).count) of \(model.items.count) · \(ReportRequest.size(model.total))")
                    .font(theme.uiSmall)
                    .monospacedDigit()
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach($model.items) { $item in
                        ReportFileRow(item: $item) { model.preview = item.url }
                            .frame(height: height(item))
                            .id(item.id)
                    }
                }
            }
            .frame(height: model.items.prefix(Self.visibleRows).reduce(0) { $0 + height($1) })
            .clipped()
            .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.surface2))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(theme.border, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: theme.radius.m))
        }
    }

    private func height(_ item: ReportModel.Item) -> CGFloat {
        item.agent || !item.warning.isEmpty ? theme.rowHeight2 : theme.rowHeight
    }

    private var footer: some View {
        HStack(spacing: theme.spacing.s) {
            if let blocked = model.blocked {
                Text(blocked)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.danger)
                    .lineLimit(1)
                    .help(blocked)
            }
            Spacer(minLength: theme.spacing.s)
            CardButton(label: "Cancel", shortcut: "⎋", isDefault: true) { model.cancel() }
            CardButton(label: "Send", tone: .primary, disabled: model.blocked != nil) { model.send() }
        }
    }
}

private struct ReportFileRow: View {
    @Binding var item: ReportModel.Item
    let open: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            Toggle(isOn: $item.checked) {
                VStack(alignment: .leading, spacing: 0) {
                    Text(item.name)
                        .font(theme.ui)
                        .foregroundStyle(item.checked ? theme.foreground : theme.muted)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .help(item.name)
                    if item.agent || !item.warning.isEmpty {
                        detail
                    }
                }
            }
            .toggleStyle(.checkbox)
            .controlSize(.small)
            .layoutPriority(1)
            Spacer(minLength: theme.spacing.s)
            Text(ReportRequest.size(item.bytes))
                .font(theme.uiSmall)
                .monospacedDigit()
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .fixedSize()
            Button(action: open) {
                Image(systemName: "eye")
                    .font(theme.uiSmall)
                    .foregroundStyle(item.url == nil ? theme.faint : theme.text2)
                    .frame(width: theme.control, height: theme.control)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(item.url == nil)
            .help("Quick Look \(item.name)")
        }
        .padding(.horizontal, theme.spacing.m)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .background(hovering ? theme.hover : .clear)
        .onHover { hovering = $0 }
    }

    private var detail: some View {
        HStack(spacing: theme.spacing.xs) {
            if !item.warning.isEmpty {
                Image(systemName: "exclamationmark.triangle.fill")
                Text(item.warning)
                    .lineLimit(1)
                    .fixedSize()
                if !item.source.isEmpty { Text("·") }
            }
            Text(item.source)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.path)
                .help(item.source)
        }
        .font(theme.uiSmall)
        .foregroundStyle(theme.warning)
    }
}
