import Foundation

@_exported import ClubmateProtocol

extension JSONValue {
    public func decode<T: Decodable>(_ type: T.Type) throws -> T {
        try decode(type, using: .companion)
    }
}

extension JSONDecoder {
    public static var companion: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let text = try decoder.singleValueContainer().decode(String.self)
            if let date = Date.parseRFC3339(text) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "bad date \(text)"))
        }
        return decoder
    }
}
