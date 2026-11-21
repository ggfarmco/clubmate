import SwiftUI
import UIKit

extension EnvironmentValues {
    @Entry var documentHeld = false
}

struct PushSettled: UIViewRepresentable {
    let done: () -> Void

    func makeUIView(context: Context) -> Probe {
        Probe(done: done)
    }

    func updateUIView(_ view: Probe, context: Context) {
        view.done = done
    }

    final class Probe: UIView {
        var done: () -> Void
        private var waited = false

        init(done: @escaping () -> Void) {
            self.done = done
            super.init(frame: .zero)
            isUserInteractionEnabled = false
        }

        required init?(coder: NSCoder) {
            nil
        }

        override func didMoveToWindow() {
            super.didMoveToWindow()
            guard window != nil, !waited else { return }
            waited = true
            var responder = next
            while let current = responder, !(current is UIViewController) { responder = current.next }
            guard let coordinator = (responder as? UIViewController)?.transitionCoordinator else {
                return DispatchQueue.main.async { [weak self] in self?.done() }
            }
            coordinator.animate(alongsideTransition: nil) { [weak self] _ in self?.done() }
        }
    }
}
