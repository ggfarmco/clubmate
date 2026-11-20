import CryptoKit
import Foundation

import CZlib

public struct TunnelMessage: Equatable, Sendable {
    public var type: TunnelType
    public var flags: UInt8
    public var msg: UInt32
    public var re: UInt32
    public var seq: UInt32
    public var plain: Data

    public init(type: TunnelType, flags: UInt8, msg: UInt32, re: UInt32 = 0, seq: UInt32 = 0, plain: Data) {
        self.type = type
        self.flags = flags
        self.msg = msg
        self.re = re
        self.seq = seq
        self.plain = plain
    }

    public func answers(_ re: UInt32, seq: UInt32) -> Bool {
        type != .request && self.re == re && self.seq == seq
    }
}

public struct TunnelRequestHead: Codable, Equatable, Sendable {
    public var method: String
    public var path: String
    public var header: [String: String]?
    public var z: Int?

    enum CodingKeys: String, CodingKey {
        case method = "m"
        case path = "p"
        case header = "h"
        case z
    }

    public init(method: String, path: String, header: [String: String]? = nil, z: Int? = nil) {
        self.method = method
        self.path = path
        self.header = header
        self.z = z
    }

    public var deflate: Bool { z != 0 }
}

public struct TunnelReplyHead: Codable, Equatable, Sendable {
    public var status: Int
    public var header: [String: String]?

    enum CodingKeys: String, CodingKey {
        case status = "s"
        case header = "h"
    }

    public init(status: Int, header: [String: String]? = nil) {
        self.status = status
        self.header = header
    }
}

public enum TunnelPack {
    public static let minDeflate = 128
    public static let maxMessage = 64 << 20
    public static let dictV1SHA256 = "c6d0cd4f01096508ea5cc43fd4d712a96b8410fcc5e0f1121e97eeff4c60d4a0"

    static let deflateLevel: Int32 = 6
    static let lengthSize = 4
    static let smallBucket = 64
    static let smallLimit = 1024

    static let dictV1: Data? = {
        guard let url = Bundle.module.url(forResource: "dict-v1", withExtension: "bin"),
              let data = try? Data(contentsOf: url),
              SHA256.hash(data: data).map({ String(format: "%02x", $0) }).joined() == dictV1SHA256
        else { return nil }
        return data
    }()

    public static func dictionary(_ id: Int) throws -> Data? {
        switch id {
        case Tunnel.dictNone: return nil
        case Tunnel.dictV1:
            guard let d = dictV1 else { throw TunnelError.dictionary(id) }
            return d
        default: throw TunnelError.dictionary(id)
        }
    }

    public static func bucket(_ n: Int) -> Int {
        if n <= smallLimit { return max(smallBucket, (n + smallBucket - 1) / smallBucket * smallBucket) }
        var b = smallLimit
        while b < n { b += b / 20 }
        return b
    }

    public static func pack(head: Data?, body: Data, dict: Int, deflate: Bool) throws -> (plain: Data, flags: UInt8) {
        var content = body
        if let head {
            guard !head.contains(0x0A) else { throw TunnelError.pack }
            content = head + Data([0x0A]) + body
        }
        var flags: UInt8 = 0
        if deflate && content.count >= minDeflate {
            let z = try compress(content, dict: dict)
            if z.count < content.count {
                content = z
                flags = TunnelHeader.deflate | UInt8(dict) << TunnelHeader.dictShift
            }
        }
        let size = bucket(lengthSize + content.count)
        guard size <= maxMessage else { throw TunnelError.tooLarge }
        var out = Data(capacity: size)
        out.appendBE(UInt32(content.count))
        out += content
        out.append(Data(count: size - out.count))
        return (out, flags)
    }

    public static func packRequest(_ head: TunnelRequestHead, body: Data, dict: Int) throws -> (plain: Data, flags: UInt8) {
        try pack(head: encode(head), body: body, dict: dict, deflate: head.deflate)
    }

    public static func packReply(_ head: TunnelReplyHead, body: Data, dict: Int, deflate: Bool) throws -> (plain: Data, flags: UInt8) {
        try pack(head: encode(head), body: body, dict: dict, deflate: deflate)
    }

    public static func unpack(_ m: TunnelMessage, dict agreed: Int) throws -> (head: Data?, body: Data) {
        let plain = Data(m.plain)
        guard plain.count >= lengthSize else { throw TunnelError.pack }
        let n = Int(plain.readBE(UInt32.self, at: 0))
        guard n <= plain.count - lengthSize else { throw TunnelError.pack }
        var content = Data(plain[lengthSize..<(lengthSize + n)])
        let dict = Int((m.flags & TunnelHeader.dictMask) >> TunnelHeader.dictShift)
        if m.flags & TunnelHeader.deflate != 0 {
            guard dict == Tunnel.dictNone || dict == agreed else { throw TunnelError.dictionary(dict) }
            content = try inflate(content, dict: dict)
        } else if dict != Tunnel.dictNone {
            throw TunnelError.pack
        }
        guard m.type == .request || m.type == .reply else { return (nil, content) }
        guard let i = content.firstIndex(of: 0x0A) else { throw TunnelError.pack }
        return (Data(content[..<i]), Data(content[(i + 1)...]))
    }

    static func encode(_ head: TunnelRequestHead) throws -> Data {
        var fields = [("m", try json(head.method)), ("p", try json(head.path))]
        if let h = head.header, !h.isEmpty { fields.append(("h", try json(h))) }
        if let z = head.z { fields.append(("z", String(z))) }
        return object(fields)
    }

    static func encode(_ head: TunnelReplyHead) throws -> Data {
        var fields = [("s", String(head.status))]
        if let h = head.header, !h.isEmpty { fields.append(("h", try json(h))) }
        return object(fields)
    }

    static func object(_ fields: [(String, String)]) -> Data {
        Data(("{" + fields.map { "\"\($0.0)\":\($0.1)" }.joined(separator: ",") + "}").utf8)
    }

    static func json<T: Encodable>(_ value: T) throws -> String {
        let e = JSONEncoder()
        e.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return String(decoding: try e.encode(value), as: UTF8.self)
            .replacingOccurrences(of: "<", with: "\\u003c")
            .replacingOccurrences(of: ">", with: "\\u003e")
            .replacingOccurrences(of: "&", with: "\\u0026")
            .replacingOccurrences(of: "\u{2028}", with: "\\u2028")
            .replacingOccurrences(of: "\u{2029}", with: "\\u2029")
    }

    static func compress(_ input: Data, dict: Int) throws -> Data {
        let d = try dictionary(dict)
        let s = UnsafeMutablePointer<z_stream>.allocate(capacity: 1)
        s.initialize(to: z_stream())
        defer {
            deflateEnd(s)
            s.deinitialize(count: 1)
            s.deallocate()
        }
        guard deflateInit2_(s, deflateLevel, Z_DEFLATED, -MAX_WBITS, 8, Z_DEFAULT_STRATEGY, zlibVersion(), Int32(MemoryLayout<z_stream>.size)) == Z_OK else { throw TunnelError.pack }
        if let d {
            let rc = d.withUnsafeBytes { deflateSetDictionary(s, $0.bindMemory(to: Bytef.self).baseAddress, uInt($0.count)) }
            guard rc == Z_OK else { throw TunnelError.pack }
        }
        var out = [UInt8](repeating: 0, count: Int(deflateBound(s, uLong(input.count))))
        let rc = input.withUnsafeBytes { inBuf in
            out.withUnsafeMutableBufferPointer { outBuf in
                s.pointee.next_in = UnsafeMutablePointer(mutating: inBuf.bindMemory(to: Bytef.self).baseAddress)
                s.pointee.avail_in = uInt(inBuf.count)
                s.pointee.next_out = outBuf.baseAddress
                s.pointee.avail_out = uInt(outBuf.count)
                return deflate(s, Z_FINISH)
            }
        }
        guard rc == Z_STREAM_END else { throw TunnelError.pack }
        return Data(out.prefix(Int(s.pointee.total_out)))
    }

    static func inflate(_ input: Data, dict: Int) throws -> Data {
        let d = try dictionary(dict)
        let s = UnsafeMutablePointer<z_stream>.allocate(capacity: 1)
        s.initialize(to: z_stream())
        defer {
            inflateEnd(s)
            s.deinitialize(count: 1)
            s.deallocate()
        }
        guard inflateInit2_(s, -MAX_WBITS, zlibVersion(), Int32(MemoryLayout<z_stream>.size)) == Z_OK else { throw TunnelError.pack }
        if let d {
            let rc = d.withUnsafeBytes { inflateSetDictionary(s, $0.bindMemory(to: Bytef.self).baseAddress, uInt($0.count)) }
            guard rc == Z_OK else { throw TunnelError.pack }
        }
        var out = Data()
        var chunk = [UInt8](repeating: 0, count: 64 << 10)
        try input.withUnsafeBytes { inBuf in
            s.pointee.next_in = UnsafeMutablePointer(mutating: inBuf.bindMemory(to: Bytef.self).baseAddress)
            s.pointee.avail_in = uInt(inBuf.count)
            while true {
                let (rc, produced) = chunk.withUnsafeMutableBufferPointer { outBuf in
                    s.pointee.next_out = outBuf.baseAddress
                    s.pointee.avail_out = uInt(outBuf.count)
                    let rc = CZlib.inflate(s, Z_NO_FLUSH)
                    return (rc, outBuf.count - Int(s.pointee.avail_out))
                }
                guard rc == Z_OK || rc == Z_STREAM_END else { throw TunnelError.pack }
                out.append(contentsOf: chunk.prefix(produced))
                guard out.count <= maxMessage else { throw TunnelError.tooLarge }
                if rc == Z_STREAM_END { return }
            }
        }
        return out
    }
}

public enum TunnelRecords {
    public static let maxRecordPlain = 64 << 10
    public static let maxPayload = maxRecordPlain - TunnelHeader.size
    public static let maxParts = 1 << 16
    public static let bindSize = 8
    public static let maxRecordWire = Tunnel.counterSize + maxRecordPlain + Tunnel.tagSize
    static let minRecordWire = Tunnel.counterSize + TunnelHeader.size + Tunnel.tagSize

    public static func split(type: TunnelType, flags: UInt8, msg: UInt32, plain: Data) throws -> [(TunnelHeader, Data)] {
        guard plain.count <= TunnelPack.maxMessage + bindSize else { throw TunnelError.tooLarge }
        let n = max(1, (plain.count + maxPayload - 1) / maxPayload)
        guard n <= maxParts else { throw TunnelError.tooLarge }
        let base = plain.startIndex
        return (0..<n).map { i in
            let lo = base + i * maxPayload, hi = base + min(plain.count, (i + 1) * maxPayload)
            let f = flags & ~TunnelHeader.last | (i == n - 1 ? TunnelHeader.last : 0)
            return (TunnelHeader(type: type, flags: f, msg: msg, part: UInt16(i)), Data(plain[lo..<hi]))
        }
    }

    public static func append(_ record: Data, to wire: inout Data) {
        wire.appendBE(UInt32(record.count))
        wire += record
    }
}

public struct TunnelAssembler: Sendable {
    var first: TunnelHeader?
    var next = 0
    var buf = Data()
    var done = false

    public init() {}

    public var started: Bool { next > 0 }

    public mutating func add(_ header: TunnelHeader, payload: Data) throws -> TunnelMessage? {
        guard !done, next < TunnelRecords.maxParts else { throw TunnelError.order }
        if let first {
            guard Int(header.part) == next, header.type == first.type, header.msg == first.msg, (header.flags ^ first.flags) & ~TunnelHeader.last == 0 else { throw TunnelError.order }
        } else {
            guard header.part == 0 else { throw TunnelError.order }
            first = header
        }
        guard buf.count + payload.count <= TunnelPack.maxMessage + TunnelRecords.bindSize else { throw TunnelError.tooLarge }
        buf += payload
        next += 1
        done = header.isLast
        guard done, let first else { return nil }
        let flags = first.flags & ~TunnelHeader.last
        guard first.type != .request else { return TunnelMessage(type: first.type, flags: flags, msg: first.msg, plain: buf) }
        guard buf.count >= TunnelRecords.bindSize else { throw TunnelError.bind }
        return TunnelMessage(type: first.type, flags: flags, msg: first.msg, re: buf.readBE(UInt32.self, at: 0), seq: buf.readBE(UInt32.self, at: 4), plain: Data(buf.dropFirst(TunnelRecords.bindSize)))
    }
}

public struct TunnelRecordReader: Sendable {
    var buffer = Data()

    public init() {}

    public var pending: Bool { !buffer.isEmpty }

    public mutating func feed(_ chunk: Data) throws -> [Data] {
        buffer += chunk
        var records: [Data] = []
        var offset = 0
        defer { buffer = Data(buffer.dropFirst(offset)) }
        while buffer.count - offset >= 4 {
            let size = Int(buffer.readBE(UInt32.self, at: offset))
            guard size >= TunnelRecords.minRecordWire, size <= TunnelRecords.maxRecordWire else { throw TunnelError.frame }
            guard buffer.count - offset >= 4 + size else { break }
            let start = buffer.startIndex + offset + 4
            records.append(Data(buffer[start..<(start + size)]))
            offset += 4 + size
        }
        return records
    }
}

public struct TunnelMessageReader: Sendable {
    let channel: TunnelChannel
    var records = TunnelRecordReader()
    var assembler = TunnelAssembler()

    public init(channel: TunnelChannel) {
        self.channel = channel
    }

    public mutating func feed(_ chunk: Data) throws -> [TunnelMessage] {
        var out: [TunnelMessage] = []
        for rec in try records.feed(chunk) {
            let (header, payload) = try channel.open(rec)
            if let m = try assembler.add(header, payload: payload) {
                out.append(m)
                assembler = TunnelAssembler()
            }
        }
        return out
    }

    public func finish() throws {
        if records.pending || assembler.started { throw TunnelError.incomplete }
    }
}

extension TunnelChannel {
    public func sealRequest(flags: UInt8, plain: Data) throws -> (wire: Data, msg: UInt32) {
        let msg = nextMsg()
        return (try sealMessage(type: .request, flags: flags, msg: msg, plain: plain), msg)
    }

    public func sealAnswer(type: TunnelType, flags: UInt8, re: UInt32, seq: UInt32, plain: Data) throws -> Data {
        guard type != .request else { throw TunnelError.bind }
        var bound = Data(capacity: TunnelRecords.bindSize + plain.count)
        bound.appendBE(re)
        bound.appendBE(seq)
        return try sealMessage(type: type, flags: flags, msg: nextMsg(), plain: bound + plain)
    }

    private func sealMessage(type: TunnelType, flags: UInt8, msg: UInt32, plain: Data) throws -> Data {
        var wire = Data()
        for (header, payload) in try TunnelRecords.split(type: type, flags: flags, msg: msg, plain: plain) {
            TunnelRecords.append(try seal(header, payload: payload), to: &wire)
        }
        return wire
    }

    public func readMessages(_ wire: Data) throws -> [TunnelMessage] {
        var r = TunnelMessageReader(channel: self)
        let out = try r.feed(wire)
        try r.finish()
        return out
    }
}
