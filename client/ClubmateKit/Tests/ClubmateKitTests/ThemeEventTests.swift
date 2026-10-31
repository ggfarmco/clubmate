import Foundation
import XCTest

import ClubmateKit

@MainActor
final class ThemeEventTests: XCTestCase {
    private var core: TestCore?
    private let event = ThemeEvent(
        name: nil,
        dark: true,
        background: "#1f1f28",
        foreground: "#dcd7ba",
        palette: Array(repeating: "#000000", count: 16)
    )

    override func tearDown() async throws {
        await core?.stop()
        core = nil
    }

    func testResolveDarkLightPair() {
        let value = " light : Light Owl , dark:Kanagawa Wave "
        XCTAssertEqual(GhosttyThemeName.resolve(value, dark: true), "Kanagawa Wave")
        XCTAssertEqual(GhosttyThemeName.resolve(value, dark: false), "Light Owl")
    }

    func testResolveBareName() {
        XCTAssertEqual(GhosttyThemeName.resolve(" Nord ", dark: false), "Nord")
    }

    func testResolveEmptyIsNil() {
        XCTAssertNil(GhosttyThemeName.resolve(nil, dark: true))
        XCTAssertNil(GhosttyThemeName.resolve("  ", dark: true))
    }

    func testThemeEventJSONKeys() throws {
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(event)) as? [String: Any])
        XCTAssertEqual(Set(object.keys), ["name", "dark", "background", "foreground", "palette"])
        XCTAssertTrue(object["name"] is NSNull)
    }

    func testHexIsLowercaseRGB() {
        XCTAssertEqual(ThemeEvent.hex(red: 0x1F, green: 0xAB, blue: 0x0C), "#1fab0c")
    }

    func testLastValueWins() {
        let config = GhosttyConfigText(["theme = Nord\n", "theme = \"dark:A,light:B\"\n"])
        XCTAssertEqual(config.last("theme"), "dark:A,light:B")
        XCTAssertNil(GhosttyConfigText(["theme = Nord\ntheme =\n"]).last("theme"))
    }

    func testPublishReachesBus() async throws {
        let core = try await TestCore.start()
        self.core = core
        let client = try await CoreClient.connect(paths: core.paths)
        let notifications = client.subscribeNotifications()
        _ = try await client.busSubscribe(pattern: ThemeEvent.topic)
        try await client.busPublish(topic: ThemeEvent.topic, payload: event)
        let notification = await first(notifications, within: .seconds(3))
        guard case .object(let params)? = notification?.params, case .object(let published)? = params["event"],
              let payload = published["payload"]
        else { return XCTFail("no bus.event: \(String(describing: notification))") }
        XCTAssertEqual(published["topic"], .string(ThemeEvent.topic))
        XCTAssertEqual(try JSONDecoder().decode(ThemeEvent.self, from: JSONEncoder().encode(payload)), event)
        await client.close()
    }
}
