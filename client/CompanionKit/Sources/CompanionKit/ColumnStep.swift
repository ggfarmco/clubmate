import Foundation

public enum ColumnStep: String, Codable, Sendable {
    case listDetail
    case detail

    public enum Room: Sendable {
        case beside
        case over

        public static let list: Double = 360
        public static let detail: Double = 640

        public init(width: Double) {
            self = width - Self.list >= Self.detail ? .beside : .over
        }
    }

    public init(from decoder: any Decoder) throws {
        self = ColumnStep(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .listDetail
    }

    public var toggled: ColumnStep {
        self == .detail ? .listDetail : .detail
    }
}
