import Foundation
import Observation

@MainActor
@Observable
public final class DocumentFind {
    public private(set) var visible = false
    public private(set) var query = ""
    public private(set) var total = 0
    public private(set) var current = 0
    public private(set) var more = false
    public private(set) var focusRequest = 0
    @ObservationIgnored weak var controller: DocumentController?

    public init() {}

    public func show() {
        #if os(iOS)
        controller?.presentFindNavigator()
        #else
        visible = true
        focusRequest += 1
        #endif
    }

    public func hide() {
        visible = false
        query = ""
        apply(.empty)
        Task { await controller?.find(.clear) }
    }

    public func search(_ text: String) async {
        query = text
        guard let result = await controller?.find(.find(text)) else { return }
        if result.query == query { apply(result) }
    }

    public func next() async {
        await step(1)
    }

    public func previous() async {
        await step(-1)
    }

    private func step(_ delta: Int) async {
        guard visible, !query.isEmpty, let result = await controller?.find(.step(delta)), result.query == query else { return }
        apply(result)
    }

    func refresh() async {
        guard visible, !query.isEmpty else { return }
        guard let result = await controller?.find(.refresh(query)), result.query == query else { return }
        apply(result)
    }

    private func apply(_ result: FindResult) {
        total = result.total
        current = result.current
        more = result.more
    }
}

struct FindResult: Equatable {
    static let empty = FindResult(query: "", total: 0, current: 0, more: false)

    var query: String
    var total: Int
    var current: Int
    var more: Bool

    init(query: String, total: Int, current: Int, more: Bool) {
        self.query = query
        self.total = total
        self.current = current
        self.more = more
    }

    init?(_ value: Any?) {
        guard let value = value as? [String: Any], let query = value["query"] as? String else { return nil }
        self.init(query: query, total: (value["total"] as? NSNumber)?.intValue ?? 0, current: (value["current"] as? NSNumber)?.intValue ?? 0, more: value["more"] as? Bool == true)
    }
}

enum FindCommand {
    case find(String)
    case step(Int)
    case refresh(String)
    case clear

    var script: (String, [String: Any]) {
        switch self {
        case .find(let query): ("return window.clubmateFind?.find(query)", ["query": query])
        case .step(let delta): ("return window.clubmateFind?.step(delta)", ["delta": delta])
        case .refresh(let query): ("return window.clubmateFind?.refresh(query)", ["query": query])
        case .clear: ("return window.clubmateFind?.clear()", [:])
        }
    }
}
