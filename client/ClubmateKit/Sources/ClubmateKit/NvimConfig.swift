import Foundation
import Observation

public enum NvimConfigMode: String, Sendable, CaseIterable {
    case shared
    case clubmate
}

public enum NvimBinary: String, Sendable, CaseIterable {
    case bundled
    case system
}

public struct NvimConfigInfo: Equatable, Sendable {
    public var mode: NvimConfigMode
    public var binary: NvimBinary
    public let bundledVersion: String
    public let systemPath: String
    public let systemVersion: String
    public let app: String
    public let configDir: String
    public let dataDir: String
    public let configExists: Bool
    public let dataExists: Bool
    public let sourceConfigBytes: Int64
    public let sourceDataBytes: Int64

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let mode = fields["mode"]?.stringValue.flatMap(NvimConfigMode.init(rawValue:)),
              let app = fields["app"]?.stringValue, let configDir = fields["configDir"]?.stringValue,
              let dataDir = fields["dataDir"]?.stringValue else { return nil }
        self.mode = mode
        binary = fields["binary"]?.stringValue.flatMap(NvimBinary.init(rawValue:)) ?? .bundled
        bundledVersion = fields["bundledVersion"]?.stringValue ?? ""
        systemPath = fields["systemPath"]?.stringValue ?? ""
        systemVersion = fields["systemVersion"]?.stringValue ?? ""
        self.app = app
        self.configDir = configDir
        self.dataDir = dataDir
        configExists = fields["configExists"]?.boolValue ?? false
        dataExists = fields["dataExists"]?.boolValue ?? false
        sourceConfigBytes = Int64(fields["sourceConfigBytes"]?.numberValue ?? -1)
        sourceDataBytes = Int64(fields["sourceDataBytes"]?.numberValue ?? -1)
    }

    public var bundledLabel: String {
        let version = Self.displayVersion(bundledVersion)
        return version.isEmpty ? "Built-in" : "Built-in (\(version))"
    }

    public var systemCaption: String {
        if systemPath.isEmpty { return "System: not found in PATH" }
        let version = Self.displayVersion(systemVersion)
        return version.isEmpty ? "System: \(systemPath)" : "System: \(systemPath) — \(version)"
    }

    private static func displayVersion(_ raw: String) -> String {
        raw.hasPrefix("NVIM v") ? String(raw.dropFirst("NVIM v".count)) : raw
    }
}

public struct NvimCopyProgress: Equatable, Sendable {
    public let copied: Int64
    public let total: Int64

    public init(copied: Int64, total: Int64) {
        self.copied = copied
        self.total = total
    }

    public var fraction: Double? {
        total > 0 ? min(Double(copied) / Double(total), 1) : nil
    }
}

public struct NvimWorkspace: Equatable, Sendable {
    public let id: String
    public let name: String
    public let session: String

    public init(id: String, name: String, session: String = "") {
        self.id = id
        self.name = name
        self.session = session
    }
}

public enum NvimRestart {
    public static let command = "editor-nvim.restart"
    static let unsavedMarker = "unsaved buffers: "

    public enum Outcome: Equatable, Sendable {
        case restarted
        case declined
        case failed(String)
    }

    @MainActor
    public static func run(
        workspace name: String,
        run: @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>,
        confirm: @MainActor (String) async -> Bool
    ) async -> Outcome {
        guard case .failure(let failure) = await run(CommandInvocation(id: command, args: .object([:]))) else { return .restarted }
        let message = NvimConfigModel.text(failure)
        guard let marker = message.range(of: unsavedMarker) else { return .failed(message) }
        guard await confirm("Neovim in “\(name)” has \(message[marker.lowerBound...]). Restart and discard the changes?") else { return .declined }
        if case .failure(let forced) = await run(CommandInvocation(id: command, args: .object(["force": .bool(true)]))) {
            return .failed(NvimConfigModel.text(forced))
        }
        return .restarted
    }
}

public enum NvimConfigSheet: Equatable, Sendable {
    case offer
    case copying
    case failed(String, useExisting: Bool)
}

@MainActor
@Observable
public final class NvimConfigModel {
    public static let configCommand = "editor-nvim.config"
    public static let copyCommand = "editor-nvim.config-copy"
    public static let statusCommand = "editor-nvim.config-copy-status"
    public static let cancelCommand = "editor-nvim.config-copy-cancel"
    public static let noWorkspace = ClubmateError.rpc(code: 0, message: "Open a workspace to change this")
    static let statusRetries = 3

    public typealias Run = @MainActor (String?, CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public private(set) var info: NvimConfigInfo?
    public private(set) var loading = false
    public private(set) var busy = false
    public private(set) var sheet: NvimConfigSheet?
    public private(set) var progress: NvimCopyProgress?
    public private(set) var error: String?
    public private(set) var needsRestart = false
    public private(set) var restartError: String?
    public private(set) var copyNotice: String?
    public private(set) var pending: [NvimWorkspace] = []

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let workspaces: @MainActor () async -> [NvimWorkspace]
    @ObservationIgnored private let displayed: @MainActor () -> Set<String>
    @ObservationIgnored private let confirm: @MainActor (String) async -> Bool
    @ObservationIgnored private var copyID: String?
    @ObservationIgnored private var copying = false
    @ObservationIgnored private var cancelRequested = false

    public init(
        run: @escaping Run,
        workspaces: @escaping @MainActor () async -> [NvimWorkspace],
        displayed: @escaping @MainActor () -> Set<String>,
        confirm: @escaping @MainActor (String) async -> Bool
    ) {
        self.run = run
        self.workspaces = workspaces
        self.displayed = displayed
        self.confirm = confirm
    }

    public static func size(_ bytes: Int64) -> String {
        guard bytes >= 0 else { return "size unknown" }
        let mb = Double(bytes) / 1_048_576
        return "\(mb.formatted(.number.precision(.fractionLength(mb < 10 ? 1 : 0)))) MB"
    }

    public func load() async {
        loading = true
        defer { loading = false }
        switch await run(nil, CommandInvocation(id: Self.configCommand, args: .object([:]))) {
        case .success(let value):
            guard let parsed = NvimConfigInfo(value) else {
                error = "unexpected reply from \(Self.configCommand)"
                return
            }
            info = parsed
            error = nil
        case .failure(let failure):
            error = Self.text(failure)
        }
    }

    public func choose(_ mode: NvimConfigMode) async {
        guard let info, !busy, sheet == nil, mode != info.mode else { return }
        if mode == .clubmate, !info.configExists {
            sheet = .offer
            return
        }
        await apply(mode)
    }

    public func chooseBinary(_ binary: NvimBinary) async {
        guard let info, !busy, sheet == nil, binary != info.binary else { return }
        busy = true
        switch await run(nil, CommandInvocation(id: Self.configCommand, args: .object(["binary": .string(binary.rawValue)]))) {
        case .success:
            self.info?.binary = binary
            error = nil
            busy = false
            await restartAll()
            await load()
        case .failure(let failure):
            error = Self.text(failure)
            busy = false
        }
    }

    public func copy() async {
        guard let info, sheet != nil, !copying else { return }
        copying = true
        cancelRequested = false
        copyNotice = nil
        sheet = .copying
        progress = nil
        defer {
            copying = false
            copyID = nil
            if sheet == .copying { sheet = nil }
        }
        let id: String
        switch await run(nil, CommandInvocation(id: Self.copyCommand, args: .object(["data": .bool(!info.dataExists)]))) {
        case .success(let value):
            guard let started = value.objectValue?["id"]?.stringValue else {
                await fail("unexpected reply from \(Self.copyCommand)")
                return
            }
            id = started
        case .failure(let failure):
            await fail(Self.text(failure))
            return
        }
        copyID = id
        if cancelRequested { await sendCancel(id) }
        var retries = 0
        while !Task.isCancelled {
            switch await run(nil, CommandInvocation(id: Self.statusCommand, args: .object(["id": .string(id)]))) {
            case .success(let value):
                retries = 0
                let fields = value.objectValue ?? [:]
                progress = NvimCopyProgress(copied: Int64(fields["copied"]?.numberValue ?? 0), total: Int64(fields["total"]?.numberValue ?? 0))
                guard fields["done"]?.boolValue == true else { continue }
                if let message = fields["error"]?.stringValue {
                    if message == "cancelled" { sheet = nil } else { await fail(message) }
                    return
                }
                sheet = nil
                await apply(.clubmate)
                return
            case .failure(let failure) where failure.isTransient && retries < Self.statusRetries:
                retries += 1
                try? await Task.sleep(for: .milliseconds(200 * retries))
            case .failure(let failure):
                await fail(Self.text(failure))
                return
            }
        }
    }

    public func cancelCopy() async {
        guard copying else { return }
        guard let copyID else {
            cancelRequested = true
            return
        }
        await sendCancel(copyID)
    }

    public func switchWithoutCopy() async {
        sheet = nil
        await apply(.clubmate)
    }

    public func dismiss() {
        guard !copying else { return }
        sheet = nil
    }

    public func restartAll() async {
        busy = true
        defer { busy = false }
        needsRestart = false
        restartError = nil
        let shown = displayed()
        let running = await workspaces()
        pending = running.filter { !shown.contains($0.id) }
        for workspace in running where shown.contains(workspace.id) {
            if !(await restart(workspace)) { needsRestart = true }
        }
    }

    public func activated(_ id: String) async {
        guard let index = pending.firstIndex(where: { $0.id == id }) else { return }
        let workspace = pending.remove(at: index)
        guard await workspaces().contains(workspace) else { return }
        if !(await restart(workspace)) { needsRestart = true }
    }

    public func forget(_ id: String) {
        pending.removeAll { $0.id == id }
    }

    private func apply(_ mode: NvimConfigMode) async {
        busy = true
        switch await run(nil, CommandInvocation(id: Self.configCommand, args: .object(["mode": .string(mode.rawValue)]))) {
        case .success:
            info?.mode = mode
            error = nil
            busy = false
            await restartAll()
            await load()
        case .failure(let failure):
            error = Self.text(failure)
            busy = false
        }
    }

    private func restart(_ workspace: NvimWorkspace) async -> Bool {
        let outcome = await NvimRestart.run(workspace: workspace.name, run: { await self.run(workspace.id, $0) }, confirm: confirm)
        if case .failed(let message) = outcome { restartError = message }
        return outcome == .restarted
    }

    private func sendCancel(_ id: String) async {
        if case .failure(let failure) = await run(nil, CommandInvocation(id: Self.cancelCommand, args: .object(["id": .string(id)]))) {
            copyNotice = "Cancel failed: \(Self.text(failure))"
        }
    }

    private func fail(_ message: String) async {
        await load()
        sheet = .failed(message, useExisting: info?.configExists ?? false)
    }

    static func text(_ error: ClubmateError) -> String {
        guard case .rpc(_, let message) = error else { return error.message }
        if message.hasPrefix("plugin "), let range = message.range(of: " command.run: ") {
            return String(message[range.upperBound...])
        }
        return message
    }
}
