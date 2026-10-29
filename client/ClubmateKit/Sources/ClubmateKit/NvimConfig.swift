import Foundation
import Observation

public enum NvimConfigMode: String, Sendable, CaseIterable {
    case shared
    case clubmate
}

public struct NvimConfigInfo: Equatable, Sendable {
    public var mode: NvimConfigMode
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
        self.app = app
        self.configDir = configDir
        self.dataDir = dataDir
        configExists = fields["configExists"]?.boolValue ?? false
        dataExists = fields["dataExists"]?.boolValue ?? false
        sourceConfigBytes = Int64(fields["sourceConfigBytes"]?.numberValue ?? -1)
        sourceDataBytes = Int64(fields["sourceDataBytes"]?.numberValue ?? -1)
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

    public init(id: String, name: String) {
        self.id = id
        self.name = name
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
    public static let restartCommand = "editor-nvim.restart"
    public static let noWorkspace = ClubmateError.rpc(code: 0, message: "Open a workspace to change this")
    static let statusRetries = 3
    static let unsavedMarker = "unsaved buffers: "

    public typealias Run = @MainActor (String?, CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public private(set) var info: NvimConfigInfo?
    public private(set) var loading = false
    public private(set) var busy = false
    public private(set) var sheet: NvimConfigSheet?
    public private(set) var progress: NvimCopyProgress?
    public private(set) var error: String?
    public private(set) var needsRestart = false
    public private(set) var copyNotice: String?

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let workspaces: @MainActor () async -> [NvimWorkspace]
    @ObservationIgnored private let confirm: @MainActor (String) async -> Bool
    @ObservationIgnored private var copyID: String?
    @ObservationIgnored private var copying = false
    @ObservationIgnored private var cancelRequested = false

    public init(
        run: @escaping Run,
        workspaces: @escaping @MainActor () async -> [NvimWorkspace],
        confirm: @escaping @MainActor (String) async -> Bool
    ) {
        self.run = run
        self.workspaces = workspaces
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
            info = NvimConfigInfo(value)
            error = info == nil ? "unexpected reply from \(Self.configCommand)" : nil
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
                fail("unexpected reply from \(Self.copyCommand)")
                return
            }
            id = started
        case .failure(let failure):
            fail(Self.text(failure))
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
                    if message == "cancelled" { sheet = nil } else { fail(message) }
                    return
                }
                sheet = nil
                await apply(.clubmate)
                return
            case .failure(let failure) where failure.isTransient && retries < Self.statusRetries:
                retries += 1
                try? await Task.sleep(for: .milliseconds(200 * retries))
            case .failure(let failure):
                fail(Self.text(failure))
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
        for workspace in await workspaces() {
            if !(await restart(workspace)) { needsRestart = true }
        }
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
        switch await run(workspace.id, CommandInvocation(id: Self.restartCommand, args: .object([:]))) {
        case .success:
            return true
        case .failure(let failure):
            let message = Self.text(failure)
            guard let marker = message.range(of: Self.unsavedMarker) else {
                error = message
                return false
            }
            guard await confirm("Neovim in “\(workspace.name)” has \(message[marker.lowerBound...]). Restart and discard the changes?") else { return false }
            if case .failure(let forced) = await run(workspace.id, CommandInvocation(id: Self.restartCommand, args: .object(["force": .bool(true)]))) {
                error = Self.text(forced)
                return false
            }
            return true
        }
    }

    private func sendCancel(_ id: String) async {
        if case .failure(let failure) = await run(nil, CommandInvocation(id: Self.cancelCommand, args: .object(["id": .string(id)]))) {
            copyNotice = "Cancel failed: \(Self.text(failure))"
        }
    }

    private func fail(_ message: String) {
        let existing = info.map { message.contains("\($0.configDir) already exists") } ?? false
        sheet = .failed(message, useExisting: existing)
    }

    static func text(_ error: ClubmateError) -> String {
        guard case .rpc(_, let message) = error else { return error.message }
        if message.hasPrefix("plugin "), let range = message.range(of: " command.run: ") {
            return String(message[range.upperBound...])
        }
        return message
    }
}
