import PhotosUI
import QuickLook
import SwiftUI

import CompanionKit

struct ReportView: View {
    let sheet: ReportSheet
    @Environment(CompanionModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var text = ""
    @State private var files: [ReportFile] = []
    @State private var mac = true
    @State private var photo: PhotosPickerItem?
    @State private var preview: URL?
    @State private var failure: String?
    @State private var loaded = false

    var body: some View {
        NavigationStack {
            if let connection = model.connection(sheet.computer) {
                content(connection)
            } else {
                ContentUnavailableView("Mac Removed", systemImage: "desktopcomputer")
            }
        }
        .onDisappear { Self.clearPreviews() }
        .task {
            guard !loaded else { return }
            loaded = true
            Self.clearPreviews()
            files = model.reportFiles(computer: sheet.computer, screenshot: nil)
            if let about = sheet.about { text = about + "\n\n" }
        }
    }

    private func content(_ connection: ComputerConnection) -> some View {
        let name = connection.computer.name
        return List {
            Section {
                TextField("Describe what went wrong", text: $text, axis: .vertical)
                    .lineLimit(4...12)
                    .accessibilityIdentifier("report.text")
            } header: {
                Text("What happened?")
            } footer: {
                if tooLong {
                    Text("Too long — keep it under 20 KB.")
                        .foregroundStyle(Color.dangerInk)
                }
            }
            Section {
                ForEach($files) { $file in
                    FileRow(file: $file) { look(at: file) }
                        .listRowDensity()
                }
                PhotosPicker(selection: $photo, matching: .screenshots) {
                    Label(hasScreenshot ? "Replace Screenshot" : "Add Screenshot", systemImage: "photo")
                }
                .accessibilityIdentifier("report.screenshot")
            } header: {
                Text("From this phone")
            } footer: {
                Text(filesFooter)
                    .foregroundStyle(tooBig ? Color.dangerInk : Color.captionInk)
            }
            Section {
                Toggle(isOn: $mac) {
                    Text("\(name)'s logs")
                }
                .tint(.accentColor)
                .accessibilityIdentifier("report.mac")
            } footer: {
                Text("Added by Clubmate on \(name) when the report goes out: " + ProblemReport.macFiles.joined(separator: ", ") + ". No sessions, workspaces or settings.")
            }
            if let line = sendLine(connection) {
                Section {
                } footer: {
                    Text(line)
                        .foregroundStyle(connection.state == .unauthorized || failure != nil ? Color.dangerInk : Color.captionInk)
                        .accessibilityIdentifier("report.status")
                }
            }
        }
        .listStyle(.insetGrouped)
        .environment(\.defaultMinListRowHeight, 44)
        .navigationTitle("Report a Problem")
        .navigationBarTitleDisplayMode(.inline)
        .navigationSubtitle(name + " · " + connection.presence)
        .quickLookPreview($preview)
        .onChange(of: photo) { _, item in
            guard let item else { return }
            Task { await attach(item) }
        }
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            ToolbarItem(placement: .confirmationAction) {
                Button("Send") { send() }
                    .disabled(!canSend(connection))
                    .accessibilityIdentifier("report.send")
            }
        }
    }

    private var trimmed: String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var tooLong: Bool {
        trimmed.utf8.count > ProblemReport.maxTextBytes
    }

    private var tooBig: Bool {
        ProblemReport.tooBig(files)
    }

    private var hasScreenshot: Bool {
        files.contains { $0.name == ProblemReport.screenshotName }
    }

    private var filesFooter: String {
        let size = ByteCountFormatter.string(fromByteCount: Int64(ProblemReport.size(files)), countStyle: .file)
        if tooBig { return "\(size) ticked — over 11 MB. Untick a file or remove the screenshot." }
        return "\(size) ticked. Tap the eye to look at a file before it goes."
    }

    private func canSend(_ connection: ComputerConnection) -> Bool {
        !trimmed.isEmpty && !tooLong && !tooBig && connection.state != .unauthorized
    }

    private func sendLine(_ connection: ComputerConnection) -> String? {
        if let failure { return failure }
        let name = connection.computer.name
        switch connection.state {
        case .unauthorized: return "Pair \(name) again to send a report."
        case .online: return "Goes to the Clubmate developers through \(name)."
        case .offline, .connecting: return "\(name) is offline · the report waits in the outbox and goes out when it's back."
        }
    }

    private func send() {
        do {
            try model.sendReport(computer: sheet.computer, text: trimmed, files: files, mac: mac)
            dismiss()
        } catch {
            failure = "Couldn't save the report on this phone: \(error.localizedDescription)"
        }
    }

    private func attach(_ item: PhotosPickerItem) async {
        guard let data = try? await item.loadTransferable(type: Data.self), let jpeg = UIImage(data: data)?.jpegData(compressionQuality: 0.8) else {
            failure = "Couldn't read that picture."
            return
        }
        files.removeAll { $0.name == ProblemReport.screenshotName }
        files.append(ReportFile(name: ProblemReport.screenshotName, data: jpeg))
    }

    private static let previews = FileManager.default.temporaryDirectory.appending(path: "report-preview", directoryHint: .isDirectory)

    private static func clearPreviews() {
        try? FileManager.default.removeItem(at: previews)
    }

    private func look(at file: ReportFile) {
        let url = Self.previews.appending(path: file.name.replacingOccurrences(of: "phone/", with: ""))
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        guard (try? file.data.write(to: url, options: .atomic)) != nil else { return }
        preview = url
    }
}

private struct FileRow: View {
    @Binding var file: ReportFile
    let look: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Button {
                file.checked.toggle()
            } label: {
                HStack(spacing: 12) {
                    Image(systemName: file.checked ? "checkmark.circle.fill" : "circle")
                        .font(.title3)
                        .foregroundStyle(file.checked ? AnyShapeStyle(.tint) : AnyShapeStyle(.tertiary))
                        .frame(width: 22)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: file.name)
                            .font(.subheadline.monospaced())
                            .foregroundStyle(Color.primary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        Text(detail)
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                            .lineLimit(2)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(file.checked ? .isSelected : [])
            .accessibilityIdentifier("report.file." + file.name)
            Button(action: look) {
                Image(systemName: "eye")
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("Look at \(file.name)")
            .accessibilityIdentifier("report.look." + file.name)
        }
    }

    private var detail: String {
        let size = ByteCountFormatter.string(fromByteCount: Int64(file.data.count), countStyle: .file)
        return [size, file.note].compactMap { $0 }.joined(separator: " · ")
    }
}
