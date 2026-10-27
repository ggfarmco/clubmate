import Darwin
import Foundation

public enum ClubmateError: Error, Equatable {
    case insecureRuntime(String)
    case untrustedPeer
    case notRunning
    case timeout
    case rpc(code: Int, message: String)
    case protocolError(String)
    case coreStartFailed(String)

    public var message: String {
        switch self {
        case .insecureRuntime(let detail): "insecure runtime: \(detail)"
        case .untrustedPeer: "untrusted core peer"
        case .notRunning: "core is not running"
        case .timeout: "request timed out"
        case .rpc(let code, let message): "\(message) (\(code))"
        case .protocolError(let detail): "protocol error: \(detail)"
        case .coreStartFailed(let detail): "core start failed: \(detail)"
        }
    }

    var isTransient: Bool {
        self == .notRunning || self == .timeout
    }
}

public enum Security {
    public static func checkRuntime(_ paths: ClubmatePaths) throws {
        for dir in [paths.runtime, paths.sessionsSocketDir] {
            try checkPrivate(dir.fileSystemPath)
        }
    }

    public static func verifyPeer(fd: Int32) throws {
        var cred = xucred()
        var len = socklen_t(MemoryLayout<xucred>.size)
        guard getsockopt(fd, SOL_LOCAL, LOCAL_PEERCRED, &cred, &len) == 0,
              cred.cr_version == XUCRED_VERSION,
              cred.cr_uid == getuid()
        else {
            throw ClubmateError.untrustedPeer
        }
    }

    private static func checkPrivate(_ path: String) throws {
        var st = stat()
        guard lstat(path, &st) == 0 else {
            if errno == ENOENT { throw ClubmateError.notRunning }
            throw ClubmateError.insecureRuntime(path)
        }
        guard st.st_mode & S_IFMT == S_IFDIR, st.st_uid == getuid(), st.st_mode & 0o777 == 0o700 else {
            throw ClubmateError.insecureRuntime(path)
        }
    }
}

extension URL {
    var fileSystemPath: String {
        let path = self.path(percentEncoded: false)
        guard path.count > 1, path.hasSuffix("/") else { return path }
        return String(path.reversed().drop { $0 == "/" }.reversed())
    }
}
