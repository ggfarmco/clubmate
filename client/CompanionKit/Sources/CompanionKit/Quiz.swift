import Foundation

import ClubmateProtocol

public enum QuizCommand {
    public static let answer = "agents.quiz-answer"
}

public struct Quiz: Decodable, Sendable, Equatable {
    public struct Line: Sendable, Equatable {
        public let n: Int
        public let text: String
        public let tone: String?
        public let label: String?
    }

    public enum Row: Decodable, Sendable, Equatable, Identifiable {
        case line(Line)
        case fold(String)

        public var id: String {
            switch self {
            case .line(let line): "line-\(line.n)"
            case .fold(let label): "fold-" + label
            }
        }

        enum CodingKeys: String, CodingKey {
            case n, text, tone, label, fold
        }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            if let fold = try c.decodeIfPresent(String.self, forKey: .fold) {
                self = .fold(fold)
                return
            }
            self = .line(Line(
                n: try c.decode(Int.self, forKey: .n),
                text: try c.decodeIfPresent(String.self, forKey: .text) ?? "",
                tone: try c.decodeIfPresent(String.self, forKey: .tone),
                label: try c.decodeIfPresent(String.self, forKey: .label)
            ))
        }
    }

    public struct Open: Decodable, Sendable, Equatable {
        public let label: String
        public let shortcut: String?
    }

    public struct Excerpt: Decodable, Sendable, Equatable {
        public let file: String
        public let commit: String
        public let rows: [Row]
        public let pick: String
        public let picked: [Int]?
        public let cursor: Int?
        public let open: Open?

        enum CodingKeys: String, CodingKey {
            case file, commit, rows, pick, picked, cursor, open
        }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            file = try c.decodeIfPresent(String.self, forKey: .file) ?? ""
            commit = try c.decodeIfPresent(String.self, forKey: .commit) ?? ""
            rows = (try? c.decodeIfPresent([Row].self, forKey: .rows)) ?? []
            pick = try c.decodeIfPresent(String.self, forKey: .pick) ?? "none"
            picked = try c.decodeIfPresent([Int].self, forKey: .picked)
            cursor = try c.decodeIfPresent(Int.self, forKey: .cursor)
            open = try c.decodeIfPresent(Open.self, forKey: .open)
        }

        public var lines: [Line] {
            rows.compactMap { if case .line(let line) = $0 { line } else { nil } }
        }

        public var fileName: String {
            (file as NSString).lastPathComponent
        }

        public var pickable: Bool {
            pick == "line" || pick == "range"
        }

        public var pickedRange: ClosedRange<Int>? {
            guard let first = picked?.first else { return nil }
            let last = picked?.last ?? first
            return min(first, last)...max(first, last)
        }

        public var commonIndent: Int {
            lines.filter { !$0.text.allSatisfy(\.isWhitespace) }.map { $0.text.prefix { $0 == " " || $0 == "\t" }.count }.min() ?? 0
        }

        public func text(_ line: Line, dropIndent: Bool) -> String {
            guard dropIndent else { return line.text }
            let indent = commonIndent
            let lead = line.text.prefix { $0 == " " || $0 == "\t" }.count
            return String(line.text.dropFirst(min(indent, lead)))
        }

        public func window(_ count: Int) -> [Line] {
            let all = lines
            guard all.count > count else { return all }
            let anchor = pickedRange?.lowerBound ?? cursor ?? all[0].n
            let center = all.firstIndex { $0.n == anchor } ?? 0
            let start = min(max(center - count / 2, 0), all.count - count)
            return Array(all[start..<start + count])
        }

        public func step(_ from: Int, _ delta: Int) -> Int? {
            let numbers = lines.map(\.n)
            guard let index = numbers.firstIndex(of: from) else { return numbers.first }
            let next = index + delta
            return numbers.indices.contains(next) ? numbers[next] : nil
        }

        public func tap(_ n: Int, range: Bool) -> ClosedRange<Int> {
            guard range, let picked = pickedRange, picked.lowerBound == picked.upperBound, picked.lowerBound != n else { return n...n }
            return min(n, picked.lowerBound)...max(n, picked.lowerBound)
        }

        public func nudge(_ delta: Int, range: Bool) -> ClosedRange<Int>? {
            guard let picked = pickedRange else { return (cursor ?? lines.first?.n).map { $0...$0 } }
            if range {
                guard let end = step(picked.upperBound, delta), end >= picked.lowerBound else { return nil }
                return picked.lowerBound...end
            }
            return step(picked.lowerBound, delta).map { $0...$0 }
        }
    }

    public struct Option: Decodable, Sendable, Equatable, Identifiable {
        public let value: String
        public let label: String
        public let key: String
        public let on: Bool

        public var id: String { value }

        enum CodingKeys: String, CodingKey {
            case value, label, key, on
        }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            value = try c.decode(String.self, forKey: .value)
            label = try c.decodeIfPresent(String.self, forKey: .label) ?? value
            key = try c.decodeIfPresent(String.self, forKey: .key) ?? ""
            on = try c.decodeIfPresent(Bool.self, forKey: .on) ?? false
        }
    }

    public struct Level: Decodable, Sendable, Equatable, Identifiable {
        public let value: String
        public let label: String
        public let key: String

        public var id: String { value }
    }

    public struct Confidence: Decodable, Sendable, Equatable {
        public let label: String
        public let levels: [Level]
        public let value: String?
        public let enabled: Bool
        public let needsRating: Bool
        public let sends: Bool
        public let holdMs: Int
        public let hint: String?

        enum CodingKeys: String, CodingKey {
            case label, levels, value, enabled, needsRating, sends, holdMs, hint
        }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
            levels = try c.decodeIfPresent([Level].self, forKey: .levels) ?? []
            value = try c.decodeIfPresent(String.self, forKey: .value)
            enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
            needsRating = try c.decodeIfPresent(Bool.self, forKey: .needsRating) ?? false
            sends = try c.decodeIfPresent(Bool.self, forKey: .sends) ?? false
            holdMs = try c.decodeIfPresent(Int.self, forKey: .holdMs) ?? 2000
            hint = try c.decodeIfPresent(String.self, forKey: .hint)
        }

        public func level(_ value: String?) -> Level? {
            levels.first { $0.value == value }
        }
    }

    public struct Reveal: Decodable, Sendable, Equatable {
        public let verdict: String
        public let label: String
        public let yours: String
        public let confidence: String?
        public let answer: String?
        public let why: String?
        public let ref: String?
        public let flag: String?
        public let note: String?

        public var shortRef: String? {
            ref.map { ($0 as NSString).lastPathComponent }
        }

        public var sureWrong: Bool {
            flag == "sure_wrong"
        }
    }

    public struct Labels: Decodable, Sendable, Equatable {
        public var skip = "Skip"
        public var undo = "Undo"
        public var sending = "Sending"
        public var fold = "Fold"
        public var answerHint = ""
        public var you = "You:"
        public var answer = "Answer:"
        public var sayIt = "Say it in words…"
        public var openOnMac = "Open on Mac"
        public var line = "Line"
        public var range = "Range"

        enum CodingKeys: String, CodingKey {
            case skip, undo, sending, fold, answerHint, you, answer, sayIt, openOnMac, line, range
        }

        public init() {}

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            let english = Labels()
            skip = try c.decodeIfPresent(String.self, forKey: .skip) ?? english.skip
            undo = try c.decodeIfPresent(String.self, forKey: .undo) ?? english.undo
            sending = try c.decodeIfPresent(String.self, forKey: .sending) ?? english.sending
            fold = try c.decodeIfPresent(String.self, forKey: .fold) ?? english.fold
            answerHint = try c.decodeIfPresent(String.self, forKey: .answerHint) ?? english.answerHint
            you = try c.decodeIfPresent(String.self, forKey: .you) ?? english.you
            answer = try c.decodeIfPresent(String.self, forKey: .answer) ?? english.answer
            sayIt = try c.decodeIfPresent(String.self, forKey: .sayIt) ?? english.sayIt
            openOnMac = try c.decodeIfPresent(String.self, forKey: .openOnMac) ?? english.openOnMac
            line = try c.decodeIfPresent(String.self, forKey: .line) ?? english.line
            range = try c.decodeIfPresent(String.self, forKey: .range) ?? english.range
        }
    }

    public let request: String
    public let type: String
    public let state: String
    public let title: String
    public let question: String
    public let step: String?
    public let hint: String?
    public let concept: String?
    public let excerpt: Excerpt?
    public let options: [Option]
    public let confidence: Confidence?
    public let reveal: Reveal?
    public let labels: Labels
    public let dock: Bool

    enum CodingKeys: String, CodingKey {
        case request, type, state, title, question, step, hint, concept, excerpt, options, confidence, reveal, labels, dock
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        request = try c.decode(String.self, forKey: .request)
        type = try c.decodeIfPresent(String.self, forKey: .type) ?? "text"
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? "asking"
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        question = try c.decodeIfPresent(String.self, forKey: .question) ?? ""
        step = try c.decodeIfPresent(String.self, forKey: .step)
        hint = try c.decodeIfPresent(String.self, forKey: .hint)
        concept = try c.decodeIfPresent(String.self, forKey: .concept)
        excerpt = try? c.decodeIfPresent(Excerpt.self, forKey: .excerpt)
        options = (try? c.decodeIfPresent([Option].self, forKey: .options)) ?? []
        confidence = try? c.decodeIfPresent(Confidence.self, forKey: .confidence)
        reveal = try? c.decodeIfPresent(Reveal.self, forKey: .reveal)
        labels = (try? c.decodeIfPresent(Labels.self, forKey: .labels)) ?? Labels()
        dock = try c.decodeIfPresent(Bool.self, forKey: .dock) ?? false
    }

    public init?(props: [String: JSONValue]) {
        guard let quiz = try? JSONValue.object(props).decode(Quiz.self) else { return nil }
        self = quiz
    }

    public var sending: Bool { state == "sending" }
    public var typed: Bool { type == "text" || type == "value" }
}

public enum QuizFold: String, Sendable, Equatable {
    case full, peek, bar
}

public struct QuizDraft: Sendable, Equatable {
    public var fold: QuizFold?
    public var range: Bool?
    public var cursor: Int?
    public var mark: Int?
    public var rating: String?

    public init() {}

    public func selection(_ excerpt: Excerpt) -> ClosedRange<Int>? {
        guard let cursor = cursor ?? excerpt.cursor ?? excerpt.pickedRange?.lowerBound ?? excerpt.lines.first?.n else { return nil }
        let mark = mark ?? cursor
        return min(mark, cursor)...max(mark, cursor)
    }

    public mutating func move(_ delta: Int, extend: Bool, in excerpt: Excerpt) {
        guard let from = cursor ?? excerpt.cursor ?? excerpt.pickedRange?.lowerBound ?? excerpt.lines.first?.n else { return }
        if extend { mark = mark ?? from } else { mark = nil }
        cursor = excerpt.step(from, delta) ?? from
    }

    public typealias Excerpt = Quiz.Excerpt
}

public enum QuizAction: Sendable, Equatable {
    case pick(ClosedRange<Int>)
    case option(String)
    case rate(String)
    case undo
    case send(text: String, confidence: String?)
    case skip
    case open

    public var name: String {
        switch self {
        case .pick, .option: "pick"
        case .rate: "rate"
        case .undo: "undo"
        case .send: "send"
        case .skip: "skip"
        case .open: "open"
        }
    }

    public var event: String? {
        switch self {
        case .pick, .option: "quizPick"
        case .rate: "quizRate"
        case .undo: "quizUndo"
        case .skip: "quizSkip"
        case .open: "quizOpen"
        case .send: nil
        }
    }

    public func fields(request: String) -> [String: JSONValue] {
        var fields: [String: JSONValue] = ["request": .string(request)]
        switch self {
        case .pick(let lines):
            fields["from"] = .number(Double(lines.lowerBound))
            fields["to"] = .number(Double(lines.upperBound))
        case .option(let value):
            fields["option"] = .string(value)
        case .rate(let value):
            fields["confidence"] = .string(value)
        case .send(let text, let confidence):
            fields["text"] = .string(text)
            if let confidence { fields["confidence"] = .string(confidence) }
        case .undo, .skip, .open:
            break
        }
        return fields
    }

    public func args(session: String, request: String) -> [String: JSONValue] {
        fields(request: request).merging(["session": .string(session), "action": .string(name)]) { $1 }
    }
}
