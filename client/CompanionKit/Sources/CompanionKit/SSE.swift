import Foundation

public struct SSEEvent: Sendable, Equatable {
    public var id: String?
    public var event: String
    public var data: String

    public init(id: String? = nil, event: String = "message", data: String) {
        self.id = id
        self.event = event
        self.data = data
    }
}

public struct SSEParser: Sendable {
    private var buffer = Data()
    private var id: String?
    private var event = ""
    private var data: [String] = []
    private var sawField = false

    public init() {}

    public mutating func feed(_ chunk: Data) -> [SSEEvent] {
        buffer.append(chunk)
        var events: [SSEEvent] = []
        while let newline = buffer.firstIndex(where: { $0 == 0x0A || $0 == 0x0D }) {
            if buffer[newline] == 0x0D {
                let next = buffer.index(after: newline)
                if next == buffer.endIndex { break }
                let end = buffer[next] == 0x0A ? buffer.index(after: next) : next
                events += line(buffer[buffer.startIndex..<newline])
                buffer.removeSubrange(buffer.startIndex..<end)
                continue
            }
            events += line(buffer[buffer.startIndex..<newline])
            buffer.removeSubrange(buffer.startIndex...newline)
        }
        return events
    }

    private mutating func line(_ bytes: Data) -> [SSEEvent] {
        let text = String(decoding: bytes, as: UTF8.self)
        if text.isEmpty { return dispatch() }
        if text.hasPrefix(":") { return [] }
        let field: Substring
        var value: Substring
        if let colon = text.firstIndex(of: ":") {
            field = text[..<colon]
            value = text[text.index(after: colon)...]
            if value.first == " " { value = value.dropFirst() }
        } else {
            field = Substring(text)
            value = ""
        }
        switch field {
        case "id" where !value.contains("\0"): id = String(value)
        case "event": event = String(value)
        case "data":
            data.append(String(value))
            sawField = true
        default: break
        }
        return []
    }

    private mutating func dispatch() -> [SSEEvent] {
        defer {
            event = ""
            data = []
            sawField = false
        }
        guard sawField else { return [] }
        return [SSEEvent(id: id, event: event.isEmpty ? "message" : event, data: data.joined(separator: "\n"))]
    }
}
