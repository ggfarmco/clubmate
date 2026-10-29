import Foundation

public struct CommandLineTool: Sendable, Equatable {
    public enum State: Sendable, Equatable {
        case missing
        case installed
        case other(String)
    }

    public let name: String
    public let link: String
    public let target: String

    public init(flavor: String, resources: URL, binDir: String = "/usr/local/bin") {
        name = Self.name(flavor: flavor)
        link = (binDir as NSString).appendingPathComponent(name)
        target = resources.appending(path: "bin/clubmate").path(percentEncoded: false)
    }

    public static func name(flavor: String) -> String {
        flavor == "release" ? "cm" : "cm-dev"
    }

    public func state(fileManager: FileManager = .default) -> State {
        if let destination = try? fileManager.destinationOfSymbolicLink(atPath: link) {
            return destination == target ? .installed : .other(destination)
        }
        return fileManager.fileExists(atPath: link) ? .other(link) : .missing
    }

    public var installScript: String {
        let dir = (link as NSString).deletingLastPathComponent
        return "do shell script \"/bin/mkdir -p \" & quoted form of \(Self.literal(dir)) & \" && /bin/ln -sfn \" & quoted form of \(Self.literal(target)) & \" \" & quoted form of \(Self.literal(link)) with administrator privileges"
    }

    private static func literal(_ text: String) -> String {
        "\"" + text.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"") + "\""
    }
}
