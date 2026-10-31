import Foundation

public struct BusEvent: Codable, Sendable, Equatable {
    public let seq: Int
    public let topic: String
    public let workspace: String
    public let payload: JSONValue

    public init(seq: Int, topic: String, workspace: String = "", payload: JSONValue = .object([:])) {
        self.seq = seq
        self.topic = topic
        self.workspace = workspace
        self.payload = payload
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        seq = try container.decode(Int.self, forKey: .seq)
        topic = try container.decode(String.self, forKey: .topic)
        payload = try container.decodeIfPresent(JSONValue.self, forKey: .payload) ?? .null
        workspace = try container.decodeIfPresent(String.self, forKey: .workspace) ?? payload.objectValue?["workspace"]?.stringValue ?? ""
    }
}
