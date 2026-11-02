import Foundation
import Observation

@MainActor
@Observable
public final class FormState {
    public static let typingDelay = Duration.milliseconds(600)

    public var values: [String: JSONValue] = [:]
    public private(set) var submissions = 0
    public private(set) var save = UIFormSave()
    @ObservationIgnored public var autosave = false
    @ObservationIgnored private var pending: Task<Void, Never>?
    @ObservationIgnored private var touched: Set<String> = []

    public init() {}

    public var dirty: Bool { save.dirty }
    public var saving: Bool { save.saving && autosave }
    public var unsent: Bool { !touched.isEmpty }

    public func touch(_ id: String) {
        touched.insert(id)
    }

    public func draft(_ id: String) -> JSONValue? {
        !autosave && touched.contains(id) ? values[id] : nil
    }

    public func submit() {
        pending?.cancel()
        pending = nil
        submissions += 1
    }

    public func edited(after delay: Duration? = nil) {
        guard autosave else { return }
        if !save.dirty { save.edited() }
        pending?.cancel()
        guard let delay else { return submit() }
        pending = Task { [weak self] in
            try? await Task.sleep(for: delay)
            guard !Task.isCancelled else { return }
            self?.submit()
        }
    }

    public func flush() {
        if dirty { submit() }
    }

    public func sent() {
        pending?.cancel()
        pending = nil
        touched.removeAll()
        save.sent()
    }

    public func answered() {
        save.answered()
    }
}
