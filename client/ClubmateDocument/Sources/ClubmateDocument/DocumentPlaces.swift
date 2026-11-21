import Foundation

@MainActor
public final class DocumentPlaces {
    private struct Place {
        let path: String
        let anchor: String?
        let y: Double
    }

    public static let shared = DocumentPlaces()

    private var places: [String: Place] = [:]

    public init() {}

    public func place(_ key: String, for content: DocumentContent) -> Double? {
        guard let place = places[key], place.path == content.path, place.anchor == content.anchor else { return nil }
        return place.y
    }

    public func save(_ key: String, for content: DocumentContent, y: Double) {
        places[key] = Place(path: content.path, anchor: content.anchor, y: y)
    }
}
