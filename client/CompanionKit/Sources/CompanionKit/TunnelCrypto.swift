import CryptoKit
import Foundation
import Synchronization

public enum TunnelError: Error, Equatable, Sendable {
    case handshake
    case open
    case replay
    case exhausted
    case record
    case order
    case incomplete
    case tooLarge
    case pack
    case frame
    case dictionary(Int)
    case bind
}

public enum Tunnel {
    public static let version: UInt8 = 1
    public static let keySize = 32
    public static let counterSize = 8
    public static let tagSize = 16
    public static let maxRecords: UInt64 = 1 << 32
    public static let windowSize: UInt64 = 1 << 16
    public static let dictNone = 0
    public static let dictV1 = 1

    static let protocolName = "clubmate tunnel v1"
    static let infoK1 = "clubmate tunnel v1 k1"
    static let infoKeys = "clubmate tunnel v1 keys"
    static let nonceSize = 12

    public static func ticketHash(_ ticket: String) -> String {
        SHA256.hash(data: Data(ticket.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    public static func keyID(_ pub: Data) -> String {
        SHA256.hash(data: pub).prefix(8).map { String(format: "%02x", $0) }.joined()
    }

    public static func decodeKey(_ s: String) throws -> Data {
        guard !s.contains(where: { "=+/".contains($0) }) else { throw TunnelError.handshake }
        var b64 = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        b64 += String(repeating: "=", count: (4 - b64.count % 4) % 4)
        guard let key = Data(base64Encoded: b64), key.count == keySize else { throw TunnelError.handshake }
        return key
    }

    public static func encodeKey(_ pub: Data) -> String {
        pub.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
    }

    static func hash(_ parts: Data...) -> Data {
        var h = SHA256()
        for p in parts { h.update(data: p) }
        return Data(h.finalize())
    }

    static func agree(_ priv: Curve25519.KeyAgreement.PrivateKey, _ pub: Data) throws -> Data {
        guard let key = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: pub),
              let secret = try? priv.sharedSecretFromKeyAgreement(with: key)
        else { throw TunnelError.handshake }
        let raw = secret.withUnsafeBytes { Data($0) }
        guard raw.contains(where: { $0 != 0 }) else { throw TunnelError.handshake }
        return raw
    }

    static func hkdf(_ ikm: Data, salt: Data, info: Data, count: Int) -> Data {
        HKDF<SHA256>.deriveKey(inputKeyMaterial: SymmetricKey(data: ikm), salt: salt, info: info, outputByteCount: count).withUnsafeBytes { Data($0) }
    }

    static func sealOnce(_ key: Data, aad: Data, _ plain: Data) throws -> Data {
        let box = try AES.GCM.seal(plain, using: SymmetricKey(data: key), nonce: AES.GCM.Nonce(data: Data(count: nonceSize)), authenticating: aad)
        return box.ciphertext + box.tag
    }

    static func openOnce(_ key: Data, aad: Data, _ sealed: Data) throws -> Data {
        guard sealed.count >= tagSize else { throw TunnelError.handshake }
        let box = try AES.GCM.SealedBox(nonce: AES.GCM.Nonce(data: Data(count: nonceSize)), ciphertext: sealed.dropLast(tagSize), tag: sealed.suffix(tagSize))
        return try AES.GCM.open(box, using: SymmetricKey(data: key), authenticating: aad)
    }

    static func nonce(_ n: UInt64) -> AES.GCM.Nonce {
        var b = Data(count: 4)
        b.appendBE(n)
        return try! AES.GCM.Nonce(data: b)
    }

    static func checkDicts(_ dicts: [Int]) throws {
        if let bad = dicts.first(where: { $0 != dictV1 }) { throw TunnelError.dictionary(bad) }
    }
}

struct TunnelKeys: Sendable {
    var ee: Data
    var h2: Data
    var kdm: Data
    var kmd: Data
    var k2: Data

    init(es: Data, ee: Data, h1: Data, macEph: Data) {
        h2 = Tunnel.hash(h1, macEph)
        let okm = Tunnel.hkdf(es + ee, salt: h1, info: Data(Tunnel.infoKeys.utf8) + h2, count: 3 * Tunnel.keySize)
        self.ee = ee
        kdm = Data(okm.prefix(Tunnel.keySize))
        kmd = Data(okm.dropFirst(Tunnel.keySize).prefix(Tunnel.keySize))
        k2 = Data(okm.suffix(Tunnel.keySize))
    }
}

public struct TunnelHandshake: Sendable {
    let macKey: Data
    let ephemeral: Curve25519.KeyAgreement.PrivateKey
    let dicts: [Int]
    let es: Data
    let h0: Data
    let h1: Data
    let k1: Data

    public static func initiate(macKey: Data, ephemeral: Curve25519.KeyAgreement.PrivateKey = .init(), dicts: [Int] = [Tunnel.dictV1], ticketHash: String? = nil) throws -> (TunnelHandshake, Data) {
        try Tunnel.checkDicts(dicts)
        let es = try Tunnel.agree(ephemeral, macKey)
        let ephPub = ephemeral.publicKey.rawRepresentation
        let h0 = Tunnel.hash(Data(Tunnel.protocolName.utf8), macKey)
        let h1 = Tunnel.hash(h0, ephPub)
        let k1 = Tunnel.hkdf(es, salt: h0, info: Data(Tunnel.infoK1.utf8), count: Tunnel.keySize)
        let th = ticketHash.map { ",\"th\":\"\($0)\"" } ?? ""
        let hello = "{\"dicts\":[\(dicts.map(String.init).joined(separator: ","))],\"kid\":\"\(Tunnel.keyID(macKey))\"\(th)}"
        let msg1 = Data([Tunnel.version]) + ephPub + (try Tunnel.sealOnce(k1, aad: h1, Data(hello.utf8)))
        return (TunnelHandshake(macKey: macKey, ephemeral: ephemeral, dicts: dicts, es: es, h0: h0, h1: h1, k1: k1), msg1)
    }

    public func finish(channel: String, msg2: Data) throws -> TunnelChannel {
        let msg2 = Data(msg2)
        guard msg2.count >= Tunnel.keySize + Tunnel.tagSize else { throw TunnelError.handshake }
        let macEph = Data(msg2.prefix(Tunnel.keySize))
        let ee = try Tunnel.agree(ephemeral, macEph)
        let keys = TunnelKeys(es: es, ee: ee, h1: h1, macEph: macEph)
        guard let plain = try? Tunnel.openOnce(keys.k2, aad: keys.h2, Data(msg2.dropFirst(Tunnel.keySize))),
              let accept = try? JSONDecoder().decode(TunnelAccept.self, from: plain)
        else { throw TunnelError.handshake }
        let dict = accept.dict ?? Tunnel.dictNone
        guard dict == Tunnel.dictNone || dicts.contains(dict) else { throw TunnelError.dictionary(dict) }
        return TunnelChannel(id: channel, dict: dict, keyID: Tunnel.keyID(macKey), keys: keys, sendKey: keys.kdm, recvKey: keys.kmd)
    }
}

struct TunnelAccept: Codable {
    var dict: Int?
}

public enum TunnelType: UInt8, Sendable {
    case request = 1
    case reply = 2
    case chunk = 3
    case end = 4
}

public struct TunnelHeader: Equatable, Sendable {
    public static let size = 8
    public static let last: UInt8 = 1
    public static let deflate: UInt8 = 2
    static let dictShift: UInt8 = 2
    static let dictMask: UInt8 = 3 << 2

    public var type: TunnelType
    public var flags: UInt8
    public var msg: UInt32
    public var part: UInt16

    public init(type: TunnelType, flags: UInt8, msg: UInt32, part: UInt16) {
        self.type = type
        self.flags = flags
        self.msg = msg
        self.part = part
    }

    public var isLast: Bool { flags & Self.last != 0 }

    var bytes: Data {
        var b = Data([type.rawValue, flags])
        b.appendBE(msg)
        b.appendBE(part)
        return b
    }

    init(parsing b: Data) throws {
        guard b.count >= Self.size, let type = TunnelType(rawValue: b[b.startIndex]) else { throw TunnelError.record }
        let flags = b[b.startIndex + 1]
        guard flags & ~(Self.last | Self.deflate | Self.dictMask) == 0 else { throw TunnelError.record }
        self.init(type: type, flags: flags, msg: b.readBE(UInt32.self, at: 2), part: b.readBE(UInt16.self, at: 6))
    }
}

struct TunnelWindow: Sendable {
    var top: UInt64 = 0
    var bits = [UInt64](repeating: 0, count: Int(Tunnel.windowSize / 64))

    private func slot(_ n: UInt64) -> (Int, UInt64) {
        (Int((n / 64) % UInt64(bits.count)), 1 << (n % 64))
    }

    func seen(_ n: UInt64) -> Bool {
        if n == 0 || n > Tunnel.maxRecords { return true }
        if n > top { return false }
        if top - n >= Tunnel.windowSize { return true }
        let (i, bit) = slot(n)
        return bits[i] & bit != 0
    }

    mutating func mark(_ n: UInt64) -> Bool {
        if seen(n) { return false }
        if n > top {
            if n - top >= Tunnel.windowSize {
                bits = [UInt64](repeating: 0, count: bits.count)
            } else {
                for k in (top + 1)..<n {
                    let (i, bit) = slot(k)
                    bits[i] &= ~bit
                }
            }
            top = n
        }
        let (i, bit) = slot(n)
        bits[i] |= bit
        return true
    }
}

public final class TunnelChannel: Sendable {
    public let id: String
    public let dict: Int
    public let keyID: String

    let keys: TunnelKeys
    let aad: Data
    let sendKey: SymmetricKey
    let recvKey: SymmetricKey
    let send = Mutex<(counter: UInt64, msg: UInt32)>((0, 0))
    let window = Mutex(TunnelWindow())

    init(id: String, dict: Int, keyID: String, keys: TunnelKeys, sendKey: Data, recvKey: Data) {
        self.id = id
        self.dict = dict
        self.keyID = keyID
        self.keys = keys
        aad = Data(id.utf8)
        self.sendKey = SymmetricKey(data: sendKey)
        self.recvKey = SymmetricKey(data: recvKey)
    }

    public func nextMsg() -> UInt32 {
        send.withLock {
            $0.msg += 1
            return $0.msg
        }
    }

    public func seal(_ header: TunnelHeader, payload: Data) throws -> Data {
        guard payload.count <= TunnelRecords.maxPayload else { throw TunnelError.tooLarge }
        let n = try send.withLock {
            guard $0.counter < Tunnel.maxRecords else { throw TunnelError.exhausted }
            $0.counter += 1
            return $0.counter
        }
        let box = try AES.GCM.seal(header.bytes + payload, using: sendKey, nonce: Tunnel.nonce(n), authenticating: aad)
        var out = Data(capacity: Tunnel.counterSize + TunnelHeader.size + payload.count + Tunnel.tagSize)
        out.appendBE(n)
        return out + box.ciphertext + box.tag
    }

    public func open(_ record: Data) throws -> (TunnelHeader, Data) {
        let record = Data(record)
        guard record.count >= Tunnel.counterSize + TunnelHeader.size + Tunnel.tagSize else { throw TunnelError.open }
        let n = record.readBE(UInt64.self, at: 0)
        if window.withLock({ $0.seen(n) }) { throw TunnelError.replay }
        let sealed = record.dropFirst(Tunnel.counterSize)
        guard let box = try? AES.GCM.SealedBox(nonce: Tunnel.nonce(n), ciphertext: sealed.dropLast(Tunnel.tagSize), tag: sealed.suffix(Tunnel.tagSize)),
              let plain = try? AES.GCM.open(box, using: recvKey, authenticating: aad)
        else { throw TunnelError.open }
        guard window.withLock({ $0.mark(n) }) else { throw TunnelError.replay }
        let header = try TunnelHeader(parsing: plain)
        return (header, Data(plain.dropFirst(TunnelHeader.size)))
    }
}

extension Data {
    mutating func appendBE<T: FixedWidthInteger>(_ v: T) {
        Swift.withUnsafeBytes(of: v.bigEndian) { append(contentsOf: $0) }
    }

    func readBE<T: FixedWidthInteger>(_: T.Type, at offset: Int) -> T {
        var v: T = 0
        for i in 0..<MemoryLayout<T>.size {
            v = v << 8 | T(self[startIndex + offset + i])
        }
        return v
    }
}
