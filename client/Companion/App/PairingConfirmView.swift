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
                            .foregroundStyle(Color.captionInk)
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
                            .foregroundStyle(Color.warningInk)
                    }
                }
                if let relay = review.link.relay {
                    Section {
                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                            Image(systemName: "point.3.connected.trianglepath.dotted")
                                .foregroundStyle(Color.captionInk)
                            VStack(alignment: .leading, spacing: 3) {
                                Text("Through Clubmate relay")
                                Text(relay.url.host() ?? relay.url.absoluteString)
                                    .font(.footnote.monospaced())
                                    .foregroundStyle(Color.captionInk)
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                            }
                            Spacer(minLength: 8)
                            if review.changesRelay {
                                Text(review.existing?.relay == nil ? "New" : "Changed").font(.caption.weight(.semibold)).foregroundStyle(Color.warningInk)
                            }
                        }
                        .accessibilityElement(children: .combine)
                        .accessibilityIdentifier("pair.relay")
                        if review.changesKey {
                            Label("The Mac's relay key changed", systemImage: "exclamationmark.triangle.fill")
                                .foregroundStyle(Color.warningInk)
                                .accessibilityIdentifier("pair.keyChanged")
                        }
                    } header: {
                        Text("Relay")
                    } footer: {
                        Text(relayFooter)
                    }
                }
                if !review.link.hosts.isEmpty || !review.removedHosts.isEmpty {
                    Section {
                        ForEach(review.link.hosts, id: \.self) { host in
                            HStack {
                                Text(host).font(.callout.monospaced())
                                Spacer()
                                if review.addedHosts.contains(host) {
                                    Text("New").font(.caption.weight(.semibold)).foregroundStyle(Color.warningInk)
                                }
                            }
                        }
                        ForEach(review.removedHosts, id: \.self) { host in
                            HStack {
                                Text(host).font(.callout.monospaced()).strikethrough()
                                Spacer()
                                Text("Removed").font(.caption.weight(.semibold)).foregroundStyle(Color.captionInk)
                            }
                            .foregroundStyle(Color.captionInk)
                        }
                    } header: {
                        Text("Tailscale addresses")
                    } footer: {
                        if review.changesAddress {
                            Text("The Mac's addresses changed since the last pairing.")
                        }
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

    private var relayFooter: String {
        if review.changesKey {
            return "The key proves the phone talks to this Mac. Continue only if you just asked Clubmate on this Mac for a new pairing code."
        }
        if review.link.hosts.isEmpty {
            return "This Mac has no Tailscale address: the phone connects through the relay. The relay passes encrypted data and can't read it."
        }
        return "The phone uses Tailscale and falls back to the relay when Tailscale can't connect. The relay can't read the data."
    }
}
