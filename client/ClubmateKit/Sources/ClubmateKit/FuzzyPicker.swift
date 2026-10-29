import Foundation
import Observation

public struct PickerItem: Equatable, Sendable, Identifiable {
    public let id: String
    public let label: String
    public let detail: String
    public let checked: Bool

    public init(id: String, label: String, detail: String = "", checked: Bool = false) {
        self.id = id
        self.label = label
        self.detail = detail
        self.checked = checked
    }
}

public enum FuzzyRank {
    public static func rank<T>(_ items: [T], query: String, text: (T) -> String) -> [T] {
        guard !query.isEmpty else { return items }
        let needle = query.map { String($0).lowercased() }
        return items.enumerated()
            .compactMap { index, item in score(text(item), needle).map { (rank: $0, index: index, item: item) } }
            .sorted { ($0.rank, $0.index) < ($1.rank, $1.index) }
            .map(\.item)
    }

    private static func score(_ title: String, _ needle: [String]) -> Int? {
        let characters = Array(title)
        let haystack = characters.map { String($0).lowercased() }
        guard isSubsequence(needle, of: haystack) else { return nil }
        if haystack.starts(with: needle) { return 0 }
        let starts = characters.indices.map { $0 == 0 || !isWordCharacter(characters[$0 - 1]) }
        return matchesWordStarts(needle, haystack, starts) ? 1 : 2
    }

    private static func isSubsequence(_ needle: [String], of haystack: [String]) -> Bool {
        var remaining = needle[...]
        for character in haystack where character == remaining.first {
            remaining = remaining.dropFirst()
            if remaining.isEmpty { return true }
        }
        return remaining.isEmpty
    }

    private static func matchesWordStarts(_ needle: [String], _ haystack: [String], _ starts: [Bool]) -> Bool {
        var memo: [[Bool?]] = Array(repeating: Array(repeating: nil, count: haystack.count + 1), count: needle.count)
        func match(_ next: Int, _ index: Int) -> Bool {
            guard next < needle.count else { return true }
            if let known = memo[next][index] { return known }
            let found = (index < haystack.count && haystack[index] == needle[next] && match(next + 1, index + 1))
                || (index..<haystack.count).contains { starts[$0] && haystack[$0] == needle[next] && match(next + 1, $0 + 1) }
            memo[next][index] = found
            return found
        }
        return match(0, 0)
    }

    private static func isWordCharacter(_ character: Character) -> Bool {
        character.isLetter || character.isNumber
    }
}

@MainActor
@Observable
public final class FuzzyPickerModel {
    public static let refsCommand = "git.review-refs"

    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>
    public typealias Source = @MainActor (String) async -> Result<[PickerItem], ClubmateError>

    public let placeholder: String
    public private(set) var query = ""
    public private(set) var items: [PickerItem] = []
    public private(set) var selection = 0
    public private(set) var failure: String?
    public private(set) var loaded = false

    @ObservationIgnored private let source: Source
    @ObservationIgnored private let debounce: Duration
    @ObservationIgnored private var pending: Task<Void, Never>?
    @ObservationIgnored private var serial = 0
    @ObservationIgnored private var epoch = 0

    public init(placeholder: String, debounce: Duration = .milliseconds(40), source: @escaping Source) {
        self.placeholder = placeholder
        self.debounce = debounce
        self.source = source
    }

    public func update(_ query: String) {
        self.query = query
        serial += 1
        let serial = serial
        pending?.cancel()
        pending = Task { [weak self, debounce] in
            if debounce > .zero {
                do { try await Task.sleep(for: debounce) } catch { return }
            }
            await self?.fetch(query, serial)
        }
    }

    public func move(_ delta: Int) {
        guard !items.isEmpty else { return }
        selection = (selection + delta + items.count) % items.count
    }

    public func select(_ index: Int) {
        if items.indices.contains(index) { selection = index }
    }

    public func choose() async -> PickerItem? {
        let epoch = epoch
        while let task = pending {
            await task.value
            guard epoch == self.epoch else { return nil }
            if pending == task { break }
        }
        return items.indices.contains(selection) ? items[selection] : nil
    }

    public func cancel() {
        epoch += 1
        serial += 1
        pending?.cancel()
        pending = nil
    }

    private func fetch(_ query: String, _ serial: Int) async {
        guard serial == self.serial, !Task.isCancelled else { return }
        let reply = await source(query)
        guard serial == self.serial, !Task.isCancelled else { return }
        switch reply {
        case .success(let found):
            items = found
            failure = nil
        case .failure(let error):
            items = []
            failure = error.message
        }
        selection = 0
        loaded = true
    }

    public static func local(_ items: @escaping @MainActor () -> [PickerItem]) -> Source {
        { query in .success(FuzzyRank.rank(items(), query: query, text: \.label)) }
    }

    public static func refs(run: @escaping Run, kinds: Set<String>? = nil) -> Source {
        { query in
            switch await run(CommandInvocation(id: refsCommand, args: .object(["query": .string(query)]))) {
            case .success(let value):
                return .success((value.arrayValue ?? []).compactMap { ref in
                    guard let fields = ref.objectValue, let id = fields["ref"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
                    let kind = fields["kind"]?.stringValue ?? ""
                    guard kinds?.contains(kind) ?? true else { return nil }
                    return PickerItem(id: id, label: label, detail: kind)
                })
            case .failure(let error):
                return .failure(error)
            }
        }
    }
}
