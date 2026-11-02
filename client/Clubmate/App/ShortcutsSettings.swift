import AppKit
import SwiftUI

import ClubmateKit

struct ShortcutsSettings: View {
    let plugins: SettingsPlugins

    @State private var query = ""

    var body: some View {
        VStack(spacing: 0) {
            TextField("Search keys or actions", text: $query)
                .textFieldStyle(.roundedBorder)
                .padding(.horizontal, 20)
                .padding(.top, 16)
            if groups.isEmpty, pointers.isEmpty {
                SettingsNotice(text: "No shortcuts match “\(query)”")
            } else {
                Form {
                    ForEach(groups) { group in
                        Section {
                            ShortcutRows(group: group)
                        } header: {
                            ShortcutGroupHeader(title: group.title, plugin: group.id.hasPrefix(PluginStatus.shortcutGroupPrefix))
                        }
                    }
                    if !pointers.isEmpty {
                        Section("Neovim and Ghostty") {
                            ForEach(pointers) { pointer in
                                SettingsRow(title: pointer.title, detail: pointer.detail) {
                                    Button("Open") { NSWorkspace.shared.open(pointer.url) }
                                        .help(pointer.url.absoluteString)
                                }
                            }
                        }
                    }
                }
                .formStyle(.grouped)
            }
        }
    }

    private var groups: [ShortcutGroup] {
        (AppShortcuts.groups + plugins.sorted.map(\.shortcuts)).compactMap { $0.filtered(query) }
    }

    private var pointers: [ShortcutPointer] {
        let terms = query.lowercased().split(whereSeparator: \.isWhitespace)
        return ShortcutPointer.all.filter { pointer in
            terms.allSatisfy { "\(pointer.title) \(pointer.detail)".lowercased().contains($0) }
        }
    }
}

struct ShortcutRows: View {
    let group: ShortcutGroup

    var body: some View {
        ForEach(group.shortcuts) { shortcut in
            ShortcutRow(shortcut: shortcut)
        }
    }
}

private struct ShortcutRow: View {
    let shortcut: Shortcut

    var body: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text(shortcut.action)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .help(shortcut.action)
                if !shortcut.when.isEmpty {
                    Text(shortcut.when)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .help(shortcut.when)
                }
            }
            Spacer(minLength: 10)
            HStack(spacing: 4) {
                ForEach(Array(shortcut.labels.enumerated()), id: \.offset) { _, label in
                    KeyCap(label: label)
                }
            }
            .fixedSize()
        }
    }
}

private struct KeyCap: View {
    let label: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text(label)
            .font(.caption)
            .foregroundStyle(theme.text2)
            .padding(theme.spacing.badge)
            .background(theme.surface2, in: RoundedRectangle(cornerRadius: theme.radius.s))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.s).strokeBorder(theme.borderStrong))
    }
}

private struct ShortcutGroupHeader: View {
    let title: String
    let plugin: Bool

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: 6) {
            Text(title)
            if plugin {
                Text("plugin")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .padding(theme.spacing.badge)
                    .overlay(RoundedRectangle(cornerRadius: theme.radius.s).strokeBorder(theme.border))
            }
        }
    }
}

private struct ShortcutPointer: Identifiable {
    let title: String
    let detail: String
    let anchor: String

    var id: String { anchor }

    var url: URL {
        URL(string: "https://github.com/ggfarmco/clubmate/blob/main/docs/shortcuts.md#\(anchor)")!
    }

    static let all = [
        ShortcutPointer(title: "nvim keys, leader ,", detail: "Clubmate's Neovim distribution, in docs/shortcuts.md", anchor: "nvim-clubmate-distribution-leader-"),
        ShortcutPointer(title: "Ghostty bindings", detail: "Terminals keep Ghostty's own keys, except ⌘W", anchor: "app-and-windows"),
    ]
}
