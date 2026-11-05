import Foundation
import Observation

@MainActor
@Observable
public final class FormState {
    public static let typingDelay = Duration.milliseconds(600)

    public var values: [String: JSONValue] = [:]
    public var openChoice: String?
    public var needsAny: [String] = []
    public private(set) var submissions = 0
    public private(set) var save = UIFormSave()
    @ObservationIgnored public var autosave = false
    @ObservationIgnored private var pending: Task<Void, Never>?
    @ObservationIgnored private var touched: Set<String> = []
    @ObservationIgnored private var mounts: [UUID] = []

    public init() {}

    public func mount(_ view: UUID) {
        mounts.removeAll { $0 == view }
        mounts.append(view)
    }

    public func unmount(_ view: UUID) {
        mounts.removeAll { $0 == view }
    }

    public var mounted: Bool { !mounts.isEmpty }

    public func sends(_ view: UUID) -> Bool {
        mounts.last.map { $0 == view } ?? true
    }

    public var dirty: Bool { save.dirty }
    public var saving: Bool { save.saving && autosave }
    public var unsent: Bool { !touched.isEmpty }

    public var unanswered: Bool {
        !needsAny.isEmpty && !needsAny.contains { Self.holds(values[$0]) }
    }

    private static func holds(_ value: JSONValue?) -> Bool {
        switch value {
        case nil, .null?: false
        case .string(let text)?: !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        case .array(let items)?: !items.isEmpty
        default: true
        }
    }

    public func touch(_ id: String) {
        touched.insert(id)
    }

    public func draft(_ id: String) -> JSONValue? {
        !autosave && touched.contains(id) ? values[id] : nil
    }

    public func submit() {
        guard !unanswered else { return }
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
