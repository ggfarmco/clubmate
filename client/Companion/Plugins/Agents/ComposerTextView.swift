import SwiftUI
import UIKit
import UniformTypeIdentifiers

struct ComposerTextView: UIViewRepresentable {
    @Binding var text: String
    let caret: ComposerCaret
    @Binding var focused: Bool
    let placeholder: String
    let enabled: Bool
    let pasteImage: (Data) -> Void

    static let maxLines = 6

    func makeUIView(context: Context) -> UITextView {
        let view = ImagePastingTextView()
        view.font = .preferredFont(forTextStyle: .body)
        view.adjustsFontForContentSizeCategory = true
        view.backgroundColor = .clear
        view.textContainerInset = .zero
        view.textContainer.lineFragmentPadding = 0
        view.isScrollEnabled = false
        view.accessibilityIdentifier = "composer"
        view.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        view.delegate = context.coordinator
        view.pasteDelegate = context.coordinator
        view.pasteConfiguration = UIPasteConfiguration(acceptableTypeIdentifiers: (view.pasteConfiguration?.acceptableTypeIdentifiers ?? [UTType.plainText.identifier]) + [UTType.image.identifier])
        return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
        context.coordinator.parent = self
        context.coordinator.updating = true
        defer { context.coordinator.updating = false }
        let replaced = view.text != text
        if replaced { view.text = text }
        if let requested = caret.requested {
            caret.requested = nil
            if let range = Self.range(at: requested, in: view.text) { view.selectedRange = range }
        }
        if replaced { caret.offset = Self.caret(of: view) }
        view.isEditable = enabled
        view.textColor = enabled ? .label : .secondaryLabel
        view.accessibilityLabel = placeholder
        if focused != view.isFirstResponder {
            DispatchQueue.main.async {
                let wanted = context.coordinator.parent.focused
                if wanted && !view.isFirstResponder {
                    view.becomeFirstResponder()
                } else if !wanted && view.isFirstResponder {
                    view.resignFirstResponder()
                }
            }
        }
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView view: UITextView, context: Context) -> CGSize? {
        let width = proposal.width.flatMap { $0.isFinite ? $0 : nil } ?? (view.bounds.width > 0 ? view.bounds.width : proposal.replacingUnspecifiedDimensions().width)
        let content = view.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude)).height
        let limit = ceil((view.font?.lineHeight ?? 20) * CGFloat(Self.maxLines))
        if view.isScrollEnabled != (content > limit) { view.isScrollEnabled = content > limit }
        return CGSize(width: width, height: min(content, limit))
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(parent: self)
    }

    static func caret(of view: UITextView) -> Int? {
        guard let range = Range(view.selectedRange, in: view.text) else { return nil }
        return view.text.distance(from: view.text.startIndex, to: range.upperBound)
    }

    static func range(at caret: Int, in text: String) -> NSRange? {
        guard let at = text.index(text.startIndex, offsetBy: caret, limitedBy: text.endIndex) else { return nil }
        return NSRange(location: at.utf16Offset(in: text), length: 0)
    }

    final class Coordinator: NSObject, UITextViewDelegate, UITextPasteDelegate {
        var parent: ComposerTextView
        var updating = false

        init(parent: ComposerTextView) {
            self.parent = parent
        }

        func textViewDidChange(_ view: UITextView) {
            guard !updating else { return }
            parent.text = view.text
        }

        func textViewDidChangeSelection(_ view: UITextView) {
            parent.caret.offset = ComposerTextView.caret(of: view)
        }

        func textViewDidBeginEditing(_ view: UITextView) {
            if !parent.focused { parent.focused = true }
        }

        func textViewDidEndEditing(_ view: UITextView) {
            if parent.focused { parent.focused = false }
        }

        func textPasteConfigurationSupporting(_ supporting: any UITextPasteConfigurationSupporting, transform item: any UITextPasteItem) {
            let provider = item.itemProvider
            guard !provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier),
                  let type = provider.registeredTypeIdentifiers.first(where: { UTType($0)?.conforms(to: .image) == true }) else {
                item.setDefaultResult()
                return
            }
            item.setNoResult()
            _ = provider.loadDataRepresentation(forTypeIdentifier: type) { data, _ in
                guard let data else { return }
                Task { @MainActor [weak self] in self?.parent.pasteImage(data) }
            }
        }
    }
}

@MainActor
final class ComposerCaret {
    var offset: Int?
    var requested: Int?
}

final class ImagePastingTextView: UITextView {
    override func canPerformAction(_ action: Selector, withSender sender: Any?) -> Bool {
        if action == #selector(paste(_:)), isEditable, UIPasteboard.general.hasImages { return true }
        return super.canPerformAction(action, withSender: sender)
    }
}
