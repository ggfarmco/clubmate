import CryptoKit
import Foundation
import XCTest

@testable import CompanionKit

final class TunnelPackTests: XCTestCase {
    func testBucketsMatchVectors() throws {
        for b in try TunnelVectors.load().buckets {
            XCTAssertEqual(TunnelPack.bucket(b.size), b.bucket, "\(b.size)")
        }
    }

    func testUnpacksGoPacks() throws {
        for p in try TunnelVectors.load().packs {
            let m = TunnelMessage(type: try XCTUnwrap(TunnelType(rawValue: p.type)), flags: p.flags, msg: 1, plain: Data(hex: p.plain))
            let (head, body) = try TunnelPack.unpack(m, dict: p.dict)
            XCTAssertEqual(head.map { String(decoding: $0, as: UTF8.self) } ?? "", p.head, p.name)
            XCTAssertEqual(body.hex, p.body, p.name)
        }
    }

    func testHeadsEncodeToGoBytes() throws {
        for p in try TunnelVectors.load().packs where !p.head.isEmpty {
            let head = Data(p.head.utf8)
            let encoded = p.type == TunnelType.request.rawValue
                ? try TunnelPack.encode(JSONDecoder().decode(TunnelRequestHead.self, from: head))
                : try TunnelPack.encode(JSONDecoder().decode(TunnelReplyHead.self, from: head))
            XCTAssertEqual(String(decoding: encoded, as: UTF8.self), p.head, p.name)
        }
        let odd = TunnelRequestHead(method: "POST", path: "/v1/rpc?a=<b>&c", header: ["B": "x", "A": "y"])
        XCTAssertEqual(String(decoding: try TunnelPack.encode(odd), as: UTF8.self), #"{"m":"POST","p":"/v1/rpc?a=\u003cb\u003e\u0026c","h":{"A":"y","B":"x"}}"#)
    }

    func testSwiftPacksFollowGoRules() throws {
        for p in try TunnelVectors.load().packs {
            let (plain, flags) = try TunnelPack.pack(head: p.head.isEmpty ? nil : Data(p.head.utf8), body: Data(hex: p.body), dict: p.dict, deflate: p.deflate)
            XCTAssertEqual(flags, p.flags, p.name)
            XCTAssertEqual(plain.count, TunnelPack.bucket(4 + Int(plain.readBE(UInt32.self, at: 0))), p.name)
            XCTAssertTrue(plain.dropFirst(4 + Int(plain.readBE(UInt32.self, at: 0))).allSatisfy { $0 == 0 }, p.name)
            let (head, body) = try TunnelPack.unpack(TunnelMessage(type: try XCTUnwrap(TunnelType(rawValue: p.type)), flags: flags, msg: 1, plain: plain), dict: p.dict)
            XCTAssertEqual(head.map { String(decoding: $0, as: UTF8.self) } ?? "", p.head, p.name)
            XCTAssertEqual(body.hex, p.body, p.name)
        }
    }

    func testRequestHeadZeroSkipsDeflate() throws {
        let body = Data(String(repeating: "{\"id\":\"agt_1\"}", count: 40).utf8)
        let head = TunnelRequestHead(method: "POST", path: "/v1/rpc", header: ["Authorization": "Bearer cmt_x"])
        XCTAssertEqual(try TunnelPack.packRequest(head, body: body, dict: Tunnel.dictV1).flags, TunnelHeader.deflate | 1 << 2)
        var z0 = head
        z0.z = 0
        let (plain, flags) = try TunnelPack.packRequest(z0, body: body, dict: Tunnel.dictV1)
        XCTAssertEqual(flags, 0)
        let (h, b) = try TunnelPack.unpack(TunnelMessage(type: .request, flags: flags, msg: 1, plain: plain), dict: Tunnel.dictV1)
        XCTAssertEqual(try JSONDecoder().decode(TunnelRequestHead.self, from: XCTUnwrap(h)), z0)
        XCTAssertEqual(b, body)
    }

    func testUnpackRefusesDictionaryNotAgreed() throws {
        let p = try XCTUnwrap(TunnelVectors.load().packs.first { $0.name == "reply-deflate-dict" })
        let cases: [(UInt8, Int, TunnelError)] = [
            (p.flags, Tunnel.dictNone, .dictionary(1)),
            (TunnelHeader.deflate | 2 << 2, Tunnel.dictV1, .dictionary(2)),
            (1 << 2, Tunnel.dictV1, .pack),
        ]
        for (flags, agreed, want) in cases {
            XCTAssertThrowsError(try TunnelPack.unpack(TunnelMessage(type: .reply, flags: flags, msg: 1, plain: Data(hex: p.plain)), dict: agreed)) {
                XCTAssertEqual($0 as? TunnelError, want)
            }
        }
    }

    func testDictV1MatchesItsHashAndTheFixture() throws {
        let d = try XCTUnwrap(TunnelPack.dictionary(Tunnel.dictV1))
        XCTAssertEqual(Data(SHA256.hash(data: d)).hex, try TunnelVectors.load().dictV1SHA256)
        XCTAssertEqual(d, try Data(contentsOf: XCTUnwrap(Bundle.module.url(forResource: "dict-v1", withExtension: "bin", subdirectory: "Fixtures"))))
        XCTAssertEqual(d.count, 32 << 10)
    }

    func testLongMessageRoundTripsInPartsFedByteByByte() throws {
        let (dev, mac) = try TunnelTestResponder.pair()
        let plain = Data((0..<(TunnelRecords.maxPayload * 2 + 10)).map { UInt8($0 % 251) })
        let wire = try dev.sealAnswer(type: .reply, flags: TunnelHeader.deflate, re: 7, seq: 0, plain: plain) + dev.sealAnswer(type: .end, flags: 0, re: 7, seq: 1, plain: Data(count: 64))
        var reader = TunnelMessageReader(channel: mac)
        var got: [TunnelMessage] = []
        for i in stride(from: 0, to: wire.count, by: 4093) {
            got += try reader.feed(wire[i..<min(wire.count, i + 4093)])
        }
        XCTAssertNoThrow(try reader.finish())
        XCTAssertEqual(got, [TunnelMessage(type: .reply, flags: TunnelHeader.deflate, msg: 1, re: 7, seq: 0, plain: plain), TunnelMessage(type: .end, flags: 0, msg: 2, re: 7, seq: 1, plain: Data(count: 64))])
    }

    func testAnswerWithoutItsBindingIsRefused() throws {
        let (dev, mac) = try TunnelTestResponder.pair()
        var wire = Data()
        TunnelRecords.append(try dev.seal(TunnelHeader(type: .reply, flags: TunnelHeader.last, msg: 1, part: 0), payload: Data(count: 4)), to: &wire)
        XCTAssertThrowsError(try mac.readMessages(wire)) { XCTAssertEqual($0 as? TunnelError, .bind) }
        XCTAssertThrowsError(try dev.sealAnswer(type: .request, flags: 0, re: 1, seq: 0, plain: Data())) { XCTAssertEqual($0 as? TunnelError, .bind) }
    }

    func testReadRefusesBrokenMessages() throws {
        XCTAssertEqual(try TunnelTestResponder.pair().mac.readMessages(Data()), [])
        let cases: [(String, ([Data]) -> Data, TunnelError)] = [
            ("cut-off", { $0[0] + $0[1] }, .incomplete),
            ("re-ordered", { $0[1] + $0[0] + $0[2] }, .order),
            ("half record", { $0[0] + $0[1] + $0[2].prefix(10) }, .incomplete),
        ]
        for (name, build, want) in cases {
            let (dev, mac) = try TunnelTestResponder.pair()
            let parts = try records(dev.sealRequest(flags: 0, plain: Data(count: TunnelRecords.maxPayload * 2 + 1)).wire)
            XCTAssertThrowsError(try mac.readMessages(build(parts)), name) { XCTAssertEqual($0 as? TunnelError, want, name) }
        }
        let (dev, mac) = try TunnelTestResponder.pair()
        let a = try records(dev.sealRequest(flags: 0, plain: Data(count: TunnelRecords.maxPayload + 1)).wire)
        let b = try records(dev.sealRequest(flags: 0, plain: Data(count: TunnelRecords.maxPayload + 1)).wire)
        XCTAssertThrowsError(try mac.readMessages(a[0] + b[1])) { XCTAssertEqual($0 as? TunnelError, .order) }
    }

    func testAssemblerRefusesPartsPastTheLimit() throws {
        var a = TunnelAssembler()
        _ = try a.add(TunnelHeader(type: .chunk, flags: 0, msg: 1, part: 0), payload: Data())
        a.next = TunnelRecords.maxParts
        XCTAssertThrowsError(try a.add(TunnelHeader(type: .chunk, flags: TunnelHeader.last, msg: 1, part: 0), payload: Data())) {
            XCTAssertEqual($0 as? TunnelError, .order)
        }
    }

    func testRecordReaderRefusesBadLengths() throws {
        for size in [TunnelRecords.minRecordWire - 1, TunnelRecords.maxRecordWire + 1] {
            var wire = Data()
            wire.appendBE(UInt32(size))
            var r = TunnelRecordReader()
            XCTAssertThrowsError(try r.feed(wire)) { XCTAssertEqual($0 as? TunnelError, .frame) }
        }
    }

    private func records(_ wire: Data) throws -> [Data] {
        var r = TunnelRecordReader()
        return try r.feed(wire).map { rec in
            var d = Data()
            TunnelRecords.append(rec, to: &d)
            return d
        }
    }
}
