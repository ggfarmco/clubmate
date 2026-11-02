import SwiftUI

import ClubmateKit

struct SettingsRow<Control: View>: View {
    let title: String
    var detail = ""
    @ViewBuilder let control: Control

    var body: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                if !detail.isEmpty {
                    SettingsDetail(detail)
                }
            }
            Spacer(minLength: 10)
            control
        }
    }
}

struct SettingsDetail: View {
    let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        Text(text)
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
    }
}

struct SettingsStepper: View {
    let title: String
    let detail: String
    @Binding var value: Double
    let range: ClosedRange<Double>

    var body: some View {
        SettingsRow(title: title, detail: detail) {
            HStack(spacing: 6) {
                Text(settingsPoints(value))
                    .monospacedDigit()
                Stepper(title, value: $value, in: range, step: 1)
                    .labelsHidden()
            }
            .fixedSize()
        }
    }
}

struct SettingsToggle: View {
    let title: String
    var detail = ""
    @Binding var isOn: Bool

    var body: some View {
        SettingsRow(title: title, detail: detail) {
            Toggle(title, isOn: $isOn)
                .toggleStyle(.switch)
                .labelsHidden()
        }
    }
}

struct SDUISettingsRows: View {
    let tree: SDUINode
    let slot: String
    let rev: Int64
    let send: UIEventSink

    @Environment(EventBanner.self) private var banner: EventBanner?

    var body: some View {
        ForEach(rows.indices, id: \.self) { index in
            let row = rows[index]
            if let label = Self.label(row), let control = row.children.last {
                SettingsRow(title: label.title, detail: label.detail) {
                    SDUINodeView(node: control)
                        .environment(\.sduiDispatch, SDUIDispatch(slot: slot, workspaceID: "", sink: send, banner: banner))
                        .environment(\.sduiParent, slot)
                }
            } else {
                view(row)
            }
        }
    }

    private var rows: [SDUINode] {
        tree.type == "stack" && tree.string("direction") != "horizontal" ? tree.children : [tree]
    }

    private func view(_ node: SDUINode) -> some View {
        SDUIView(node: node, slot: slot, workspaceID: "", rev: rev, send: send)
            .fixedSize(horizontal: false, vertical: true)
    }

    static func label(_ row: SDUINode) -> (title: String, detail: String)? {
        guard row.type == "stack", row.string("direction") == "horizontal", row.children.count == 2, let texts = row.children.first,
              texts.type == "stack", texts.string("direction") != "horizontal", texts.children.allSatisfy({ $0.type == "text" }),
              let title = texts.children.first?.string("text") else { return nil }
        return (title, texts.children.dropFirst().compactMap { $0.string("text") }.joined(separator: " "))
    }
}
