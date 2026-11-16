import Foundation

public struct AwakeState: Codable, Equatable, Sendable {
    public var on: Bool
    public var text: String
    public var detail: String
    public var manual: Bool
    public var until: Int

    public init(on: Bool, text: String, detail: String, manual: Bool, until: Int = 0) {
        self.on = on
        self.text = text
        self.detail = detail
        self.manual = manual
        self.until = until
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        on = try c.decodeIfPresent(Bool.self, forKey: .on) ?? false
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
        manual = try c.decodeIfPresent(Bool.self, forKey: .manual) ?? false
        until = try c.decodeIfPresent(Int.self, forKey: .until) ?? 0
    }

    public var headline: String {
        on ? text : detail
    }

    public var footnote: String? {
        on && !detail.isEmpty ? detail : nil
    }
}
