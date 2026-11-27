#if DEBUG
import Foundation

import CompanionKit

enum LandQueueFixture {
    static var directory: URL? {
        UserDefaults.standard.string(forKey: "landQueueFixture").map { URL(filePath: $0) }
    }

    static var page: JSONValue? {
        read("page.json")
    }

    static func transcript(_ session: String) -> JSONValue? {
        read("transcript-\(session).json")
    }

    private static func read(_ name: String) -> JSONValue? {
        guard let url = directory?.appending(path: name), let data = try? Data(contentsOf: url) else { return nil }
        return try? JSONDecoder().decode(JSONValue.self, from: data)
    }
}
#endif
