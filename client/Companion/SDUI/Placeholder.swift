import SwiftUI

import CompanionKit

struct SDUIPlaceholder: View {
    let type: String
    @Environment(\.sduiInset) private var inset

    static func title(for type: String) -> String {
        "\(type) isn't supported on the phone"
    }

    var body: some View {
        PlaceholderBox(symbol: "questionmark.square.dashed", title: Self.title(for: type), detail: "Update Clubmate on this phone to see it.")
            .padding(.horizontal, inset)
            .accessibilityIdentifier("sdui.placeholder.\(type)")
    }
}

struct MacOnlyCaption: View {
    static let types: Set<String> = ["graph", "flow", "lanes", "runstrip"]

    let type: String
    @Environment(\.sduiInset) private var inset

    static func ghostOnly(_ node: SDUINode) -> Bool {
        guard node.type == "flow", let cards = node.props["cards"]?.arrayValue, !cards.isEmpty else { return false }
        return cards.allSatisfy { $0["ghost"]?.boolValue == true }
    }

    var body: some View {
        Label("Drawn on the Mac", systemImage: "macbook")
            .font(.footnote)
            .foregroundStyle(.tertiary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, inset)
            .accessibilityIdentifier("sdui.macOnly.\(type)")
    }
}

struct DrawnRunsSummary: View {
    let node: SDUINode
    let title: String
    @Environment(\.sduiInset) private var inset

    private static let legend: [(status: String, label: String, symbol: String, tone: String?)] = [
        ("ok", "ok", "circle.fill", "success"),
        ("failed", "failed", "xmark.circle", "danger"),
        ("missed", "missed", "circle", "warning"),
        ("running", "running", "play.fill", "accent"),
        ("ignored", "skipped", "circle.fill", nil),
    ]

    var body: some View {
        let statuses = node.props["runs"]?.arrayValue?.compactMap { $0["status"]?.stringValue } ?? []
        let count = node.props["count"]?.numberValue.map(Int.init) ?? node.props["runs"]?.arrayValue?.count
        VStack(alignment: .leading, spacing: 6) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) {
                    heading(count)
                    Spacer(minLength: 0)
                    legend(statuses)
                }
                VStack(alignment: .leading, spacing: 4) {
                    heading(count)
                    legend(statuses)
                }
            }
            MacOnlyCaption(type: node.type)
                .environment(\.sduiInset, 0)
        }
        .padding(.horizontal, inset)
        .accessibilityIdentifier("sdui.\(node.type).summary")
    }

    private func heading(_ count: Int?) -> some View {
        Text(count.map { "\(title) · \($0)" } ?? title)
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(.secondary)
    }

    private func legend(_ statuses: [String]) -> some View {
        HStack(spacing: 10) {
            ForEach(Self.legend, id: \.status) { entry in
                let n = statuses.count { $0 == entry.status }
                if n > 0 {
                    Label {
                        Text("\(entry.label) \(n)")
                            .monospacedDigit()
                    } icon: {
                        Image(systemName: entry.symbol)
                            .imageScale(entry.status == "ignored" ? .small : .medium)
                            .foregroundStyle(SDUITone.ink(entry.tone) ?? .secondary)
                    }
                    .labelStyle(.titleAndIcon)
                }
            }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .lineLimit(1)
    }
}

struct SurfacePlaceholder: View {
    @Environment(\.sduiInset) private var inset

    var body: some View {
        PlaceholderBox(symbol: "terminal", title: "Terminal — open it on the Mac", detail: nil)
            .padding(.horizontal, inset)
    }
}

private struct PlaceholderBox: View {
    let symbol: String
    let title: String
    let detail: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Image(systemName: symbol)
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.subheadline.weight(.semibold))
                if let detail {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.surface1, in: .rect(cornerRadius: 20))
        .overlay(RoundedRectangle(cornerRadius: 20).strokeBorder(.quaternary, style: StrokeStyle(lineWidth: 1, dash: [4, 3])))
    }
}
