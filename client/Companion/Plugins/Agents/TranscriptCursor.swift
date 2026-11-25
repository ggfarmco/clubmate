import SwiftUI

import CompanionKit

@MainActor
@Observable
final class FoldStore {
    static let shared = FoldStore()

    private var open: [String: [String: Bool]] = [:]

    func isOpen(_ id: String, _ initial: Bool, scope: String) -> Bool {
        open[scope]?[id] ?? initial
    }

    func set(_ id: String, _ shown: Bool, scope: String) {
        Motion.disclose { open[scope, default: [:]][id] = shown }
    }

    func prune(scope: String, keep: Set<String>) {
        guard let folds = open[scope] else { return }
        let kept = folds.filter { keep.contains($0.key) }
        if kept.count != folds.count { open[scope] = kept.isEmpty ? nil : kept }
    }
}

extension EnvironmentValues {
    @Entry var foldScope = ""
}

@MainActor
@propertyWrapper
struct Fold: DynamicProperty {
    let key: String?
    let initial: Bool
    @State private var local: Bool
    @Environment(\.foldScope) private var scope

    init(_ key: String?, initial: Bool = false) {
        self.key = key
        self.initial = initial
        _local = State(initialValue: initial)
    }

    var wrappedValue: Bool {
        get { key.map { FoldStore.shared.isOpen($0, initial, scope: scope) } ?? local }
        nonmutating set {
            if let key { FoldStore.shared.set(key, newValue, scope: scope) } else { Motion.disclose { local = newValue } }
        }
    }

    var projectedValue: Binding<Bool> {
        Binding(get: { wrappedValue }, set: { wrappedValue = $0 })
    }
}

@MainActor
@Observable
final class CursorBar {
    static let shared = CursorBar()

    var block: String?
}

struct CursorBarMark: View {
    let id: String

    var body: some View {
        Color.accentColor
            .frame(width: 2)
            .padding(.vertical, -2)
            .offset(x: -6)
            .allowsHitTesting(false)
            .accessibilityElement()
            .accessibilityLabel("Focused block")
            .accessibilityIdentifier("transcript.bar." + id)
    }
}

extension View {
    func cursorBar(_ id: String) -> some View {
        overlay(alignment: .leading) {
            if CursorBar.shared.block == id { CursorBarMark(id: id) }
        }
    }
}

struct PaneKeysSheet: View {
    let close: () -> Void

    var body: some View {
        NavigationStack {
            List {
                ForEach(PaneKeysTable.transcript, id: \.keys) { row in
                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                        Text(row.keys)
                            .font(.callout.monospaced())
                            .foregroundStyle(.tint)
                            .frame(minWidth: 96, alignment: .leading)
                        Text(row.action)
                            .font(.callout)
                    }
                }
            }
            .navigationTitle("Transcript keys")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done", action: close)
                        .keyboardShortcut(.cancelAction)
                }
            }
        }
        .accessibilityIdentifier("transcript.keys")
    }
}

struct CursorNote: View {
    let text: String
    let id: String

    var body: some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(Color.captionInk)
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .glassEffect(in: .capsule)
            .accessibilityIdentifier(id)
    }
}
