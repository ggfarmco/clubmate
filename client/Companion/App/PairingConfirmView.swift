import SwiftUI

import CompanionKit

struct PairingConfirmView: View {
    let review: PairingReview
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @Environment(PairingFlow.self) private var pairing
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(spacing: 10) {
                        Image(systemName: review.replacesExisting ? "arrow.triangle.2.circlepath" : "desktopcomputer")
                            .font(.system(size: 44))
                            .foregroundStyle(.tint)
                            .symbolRenderingMode(.hierarchical)
                        Text(review.replacesExisting ? "Replace Existing Pairing?" : "Pair with \(review.link.computerName)?")
                            .font(.title3.bold())
                            .multilineTextAlignment(.center)
                        Text(review.replacesExisting
                             ? "This phone is already paired with \(review.existing?.name ?? review.link.computerName). Pairing again replaces its access token."
                             : "Only continue if you just asked Clubmate on this Mac for a pairing code.")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                    }
                    .frame(maxWidth: .infinity)
                    .listRowBackground(Color.clear)
                }
                Section("Mac") {
                    LabeledContent("Name", value: review.link.computerName)
                    LabeledContent("Port", value: String(review.link.port))
                    if let previous = review.previousPort {
                        LabeledContent("Previous Port", value: String(previous))
                            .foregroundStyle(.orange)
                    }
                }
                Section {
                    ForEach(review.link.hosts, id: \.self) { host in
                        HStack {
                            Text(host).font(.callout.monospaced())
                            Spacer()
                            if review.addedHosts.contains(host) {
                                Text("New").font(.caption.weight(.semibold)).foregroundStyle(.orange)
                            }
                        }
                    }
                    ForEach(review.removedHosts, id: \.self) { host in
                        HStack {
                            Text(host).font(.callout.monospaced()).strikethrough()
                            Spacer()
                            Text("Removed").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                        }
                        .foregroundStyle(.secondary)
                    }
                } header: {
                    Text("Addresses")
                } footer: {
                    if review.changesAddress {
                        Text("The Mac's addresses changed since the last pairing.")
                    }
                }
            }
            .navigationTitle("Confirm Pairing")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", systemImage: "xmark") {
                        pairing.review = nil
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(review.replacesExisting ? "Replace" : "Pair", systemImage: "checkmark") {
                        pairing.confirm(model: model, navigation: navigation)
                    }
                    .buttonStyle(.glassProminent)
                }
            }
        }
    }
}
