import Foundation

public struct ReportFile: Sendable, Equatable, Identifiable {
    public let name: String
    public let data: Data
    public var checked: Bool
    public let note: String?

    public init(name: String, data: Data, checked: Bool = true, note: String? = nil) {
        self.name = name
        self.data = data
        self.checked = checked
        self.note = note
    }

    public var id: String { name }
}

public enum ProblemReport {
    public static let maxFileBytes = 11 << 20
    public static let maxTextBytes = 20 << 10
    public static let outboxNote = "contains what you typed"
    public static let macFiles = ["info.json", "logs/core.log", "logs/plugins/*.log", "plugins.json"]
    public static let screenshotName = "phone/screenshot.jpg"

    public static var appVersion: String {
        [Bundle.main.infoDictionary?["CFBundleShortVersionString"], Bundle.main.infoDictionary?["CFBundleVersion"]].compactMap { $0 as? String }.joined(separator: "+")
    }

    @MainActor
    public static func files(connection: ComputerConnection, queue: ActionQueue, device: DeviceIdentity, appVersion: String, screenshot: Data?) -> [ReportFile] {
        let computer = connection.computer
        let info = Info(app: appVersion, os: ProcessInfo.processInfo.operatingSystemVersionString, model: device.model, mac: Info.Mac(name: computer.name, colour: computer.colour, port: computer.port, state: connection.presence, version: connection.hello?.version))
        let outbox = Outbox(items: queue.items.filter { $0.computer == computer.id }, notes: queue.notes(computer: computer.id))
        var files = [
            ReportFile(name: "phone/info.json", data: encode(info)),
            ReportFile(name: "phone/events.log", data: Data(connection.events.text.utf8)),
            ReportFile(name: "phone/connection.log", data: Data(connection.log.text.utf8)),
            ReportFile(name: "phone/outbox.json", data: encode(outbox), checked: false, note: outboxNote),
        ]
        if let screenshot { files.append(ReportFile(name: screenshotName, data: screenshot)) }
        return files
    }

    public static func size(_ files: [ReportFile]) -> Int {
        files.filter(\.checked).reduce(0) { $0 + $1.data.count }
    }

    public static func tooBig(_ files: [ReportFile]) -> Bool {
        size(files) > maxFileBytes
    }

    private struct Info: Encodable {
        struct Mac: Encodable {
            let name: String
            let colour: String?
            let port: Int
            let state: String
            let version: String?
        }

        let app: String
        let os: String
        let model: String
        let mac: Mac
    }

    private struct Outbox: Encodable {
        let items: [QueuedAction]
        let notes: [QueueNote]
    }

    private static func encode(_ value: some Encodable) -> Data {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        return (try? encoder.encode(value)) ?? Data()
    }
}
