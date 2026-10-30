import SwiftUI
import VisionKit

import CompanionKit

struct AddComputerView: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @Environment(PairingFlow.self) private var pairing
    @Environment(\.dismiss) private var dismiss
    @State private var scanning = false
    @State private var typed = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(spacing: 12) {
                        Image(systemName: "macbook.and.iphone")
                            .font(.system(size: 52))
                            .foregroundStyle(.tint)
                            .symbolRenderingMode(.hierarchical)
                        Text("Pair a Mac")
                            .font(.title2.bold())
                        Text("In Clubmate on your Mac, open Settings → Devices and choose Pair Device… Both devices must be on the same Tailscale network.")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                    }
                    .frame(maxWidth: .infinity)
                    .listRowBackground(Color.clear)
                }
                Section {
                    Button {
                        scanning = true
                    } label: {
                        Label("Scan QR Code", systemImage: "qrcode.viewfinder")
                    }
                    .disabled(!QRScannerView.isAvailable)
                    PasteButton(payloadType: String.self) { strings in
                        guard let text = strings.first else { return }
                        pair(text)
                    }
                    .labelStyle(.titleAndIcon)
                } footer: {
                    if !QRScannerView.isAvailable {
                        Text("The camera scanner isn't available here. Copy the link from the Mac and paste it.")
                    }
                }
                Section("Pairing Link") {
                    TextField("clubmate://pair?…", text: $typed, axis: .vertical)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .font(.callout.monospaced())
                        .submitLabel(.go)
                        .onSubmit { pair(typed) }
                    Button("Pair") { pair(typed) }
                        .disabled(typed.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
                if let message = pairing.failureMessage {
                    Section {
                        Label(message, systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("Add Mac")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", systemImage: "xmark") { dismiss() }
                }
            }
            .fullScreenCover(isPresented: $scanning) {
                ScannerSheet { code in
                    scanning = false
                    pairing.received(code, from: .scan, model: model, navigation: navigation)
                }
            }
            .onDisappear { pairing.dismissFailure() }
            .sheet(item: Binding(get: { pairing.review }, set: { pairing.review = $0 })) { review in
                PairingConfirmView(review: review)
            }
        }
    }

    private func pair(_ text: String) {
        pairing.received(text, from: .paste, model: model, navigation: navigation)
    }
}

struct ScannerSheet: View {
    let found: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            QRScannerView(found: found)
                .ignoresSafeArea()
                .overlay(alignment: .bottom) {
                    Text("Point the camera at the code in Clubmate on your Mac.")
                        .font(.callout)
                        .padding(.horizontal, 18)
                        .padding(.vertical, 12)
                        .glassEffect()
                        .padding(.bottom, 40)
                }
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel", systemImage: "xmark") { dismiss() }
                    }
                }
        }
    }
}

struct QRScannerView: UIViewControllerRepresentable {
    let found: (String) -> Void

    static var isAvailable: Bool {
        DataScannerViewController.isSupported && DataScannerViewController.isAvailable
    }

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let scanner = DataScannerViewController(recognizedDataTypes: [.barcode(symbologies: [.qr])], qualityLevel: .balanced, isHighlightingEnabled: true)
        scanner.delegate = context.coordinator
        try? scanner.startScanning()
        return scanner
    }

    func updateUIViewController(_ controller: DataScannerViewController, context: Context) {}

    static func dismantleUIViewController(_ controller: DataScannerViewController, coordinator: Coordinator) {
        controller.stopScanning()
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(found: found)
    }

    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        let found: (String) -> Void
        private var done = false

        init(found: @escaping (String) -> Void) {
            self.found = found
        }

        func dataScanner(_ scanner: DataScannerViewController, didAdd items: [RecognizedItem], allItems: [RecognizedItem]) {
            guard !done else { return }
            for item in items {
                if case .barcode(let code) = item, let value = code.payloadStringValue, value.hasPrefix(PairingLink.scheme + "://") {
                    done = true
                    scanner.stopScanning()
                    found(value)
                    return
                }
            }
        }
    }
}
