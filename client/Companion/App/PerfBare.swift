#if DEBUG
import SwiftUI

struct PerfBareScreen: View {
    private static let seeds = Array(0..<80)

    var body: some View {
        TabView {
            Tab("Agents", systemImage: "sparkles") {
                NavigationStack {
                    List(Self.seeds, id: \.self) { seed in
                        NavigationLink(value: seed) {
                            VStack(alignment: .leading) {
                                Text("Session #\(seed)")
                                Text("Done · 20 h").font(.footnote).foregroundStyle(.secondary)
                            }
                        }
                        .accessibilityIdentifier(String(format: "row.agt_%016x", 0x900 + seed))
                    }
                    .navigationTitle("Agents")
                    .navigationDestination(for: Int.self) { seed in
                        ScrollView {
                            VStack(alignment: .leading, spacing: 10) {
                                ForEach(1...13, id: \.self) { row in
                                    Text("Turn \(row): the hold in reserve.go stays until the expiry job frees it.")
                                }
                                Text("Done with step 2 of \(seed). Tests pass.")
                            }
                            .padding()
                        }
                        .defaultScrollAnchor(.bottom)
                        .navigationTitle("Session #\(seed)")
                        .navigationBarTitleDisplayMode(.inline)
                    }
                }
            }
            Tab("Backlog", systemImage: "checklist") { Text("Backlog") }
        }
    }
}
#endif
