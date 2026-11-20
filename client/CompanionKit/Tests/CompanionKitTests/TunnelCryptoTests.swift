import CryptoKit
import Foundation
import XCTest

@testable import CompanionKit

final class TunnelCryptoTests: XCTestCase {
    func testHandshakeMatchesVectors() throws {
        let v = try TunnelVectors.load()
        let macPub = Data(hex: v.macStaticPub)
        XCTAssertEqual(try Tunnel.decodeKey(v.macKey), macPub)
        XCTAssertEqual(Tunnel.encodeKey(macPub), v.macKey)
        XCTAssertThrowsError(try Tunnel.decodeKey(Data(repeating: 0xfb, count: 32).base64EncodedString().replacingOccurrences(of: "=", with: "")))
        XCTAssertEqual(Tunnel.keyID(macPub), v.kid)
        XCTAssertEqual(try v.key(v.macStaticPriv).publicKey.rawRepresentation, macPub)

        XCTAssertEqual(Tunnel.ticketHash("cmk_vector"), v.ticketHash)
        let (hs, msg1) = try TunnelHandshake.initiate(macKey: macPub, ephemeral: v.key(v.deviceEphPriv), dicts: v.dicts, ticketHash: v.ticketHash)
        XCTAssertEqual(hs.ephemeral.publicKey.rawRepresentation.hex, v.deviceEphPub)
        XCTAssertEqual([hs.h0.hex, hs.h1.hex, hs.es.hex, hs.k1.hex], [v.h0, v.h1, v.es, v.k1])
        XCTAssertEqual(msg1.hex, v.msg1)

        let (mac, msg2) = try TunnelTestResponder.respond(channel: v.channel, msg1: msg1, statics: [Curve25519.KeyAgreement.PrivateKey(), v.key(v.macStaticPriv)], ephemeral: v.key(v.macEphPriv))
        XCTAssertEqual(msg2.hex, v.msg2)
        let dev = try hs.finish(channel: v.channel, msg2: Data(hex: v.msg2))
        XCTAssertEqual([dev.keys.ee.hex, dev.keys.h2.hex, dev.keys.k2.hex, dev.keys.kdm.hex, dev.keys.kmd.hex], [v.ee, v.h2, v.k2, v.kDM, v.kMD])
        XCTAssertEqual([dev.dict, mac.dict], [v.dict, v.dict])
        XCTAssertEqual([dev.keyID, mac.keyID], [v.kid, v.kid])
    }

    func testRecordsMatchVectors() throws {
        let v = try TunnelVectors.load()
        let (hs, _) = try TunnelHandshake.initiate(macKey: Data(hex: v.macStaticPub), ephemeral: v.key(v.deviceEphPriv), dicts: v.dicts, ticketHash: v.ticketHash)
        let dev = try hs.finish(channel: v.channel, msg2: Data(hex: v.msg2))
        let (mac, _) = try TunnelTestResponder.respond(channel: v.channel, msg1: Data(hex: v.msg1), statics: [v.key(v.macStaticPriv)], ephemeral: v.key(v.macEphPriv))
        for r in v.records {
            let (from, to) = r.dir == "dm" ? (dev, mac) : (mac, dev)
            let type = try XCTUnwrap(TunnelType(rawValue: r.type))
            let flags = r.flags & ~TunnelHeader.last
            let wire = r.dir == "dm" ? try from.sealRequest(flags: flags, plain: Data(hex: r.payload)).wire : try from.sealAnswer(type: type, flags: flags, re: XCTUnwrap(r.re), seq: XCTUnwrap(r.seq), plain: Data(hex: r.payload))
            XCTAssertEqual(wire.hex, r.wire, r.pack)
            XCTAssertEqual(try to.readMessages(Data(hex: r.wire)), [TunnelMessage(type: type, flags: flags, msg: r.msg, re: r.re ?? 0, seq: r.seq ?? 0, plain: Data(hex: r.payload))], r.pack)
            XCTAssertEqual(Data(hex: r.wire).readBE(UInt64.self, at: 4), r.counter)
        }
    }

    func testHandshakeFailsWithoutTheMacKey() throws {
        let s = Curve25519.KeyAgreement.PrivateKey()
        let (hs, msg1) = try TunnelHandshake.initiate(macKey: s.publicKey.rawRepresentation)
        XCTAssertThrowsError(try TunnelTestResponder.respond(channel: "ch", msg1: msg1, statics: [Curve25519.KeyAgreement.PrivateKey()])) {
            XCTAssertEqual($0 as? TunnelError, .handshake)
        }
        let (_, msg2) = try TunnelTestResponder.respond(channel: "ch", msg1: msg1, statics: [s])
        var forged = msg2
        forged[forged.count - 1] ^= 1
        XCTAssertThrowsError(try hs.finish(channel: "ch", msg2: forged)) { XCTAssertEqual($0 as? TunnelError, .handshake) }
    }

    func testHandshakeRefusesDictionariesNotAgreed() throws {
        XCTAssertThrowsError(try TunnelHandshake.initiate(macKey: Curve25519.KeyAgreement.PrivateKey().publicKey.rawRepresentation, dicts: [2])) {
            XCTAssertEqual($0 as? TunnelError, .dictionary(2))
        }
        let s = Curve25519.KeyAgreement.PrivateKey()
        let (hs, msg1) = try TunnelHandshake.initiate(macKey: s.publicKey.rawRepresentation, dicts: [])
        let (_, plain) = try TunnelTestResponder.respond(channel: "ch", msg1: msg1, statics: [s])
        XCTAssertEqual(try hs.finish(channel: "ch", msg2: plain).dict, Tunnel.dictNone)
        let (_, forced) = try TunnelTestResponder.respond(channel: "ch", msg1: msg1, statics: [s], accept: Tunnel.dictV1)
        XCTAssertThrowsError(try hs.finish(channel: "ch", msg2: forced)) { XCTAssertEqual($0 as? TunnelError, .dictionary(Tunnel.dictV1)) }
    }

    func testOpenRefusesTamper() throws {
        let cases: [(String, String, (inout Data) -> Void)] = [
            ("ciphertext", "ch_a", { $0[$0.count - 20] ^= 1 }),
            ("tag", "ch_a", { $0[$0.count - 1] ^= 1 }),
            ("counter", "ch_a", { $0[Tunnel.counterSize - 1] = 2 }),
            ("channel", "ch_b", { _ in }),
        ]
        for (name, macChannel, tamper) in cases {
            let (dev, mac) = try TunnelTestResponder.pair(channel: "ch_a", macChannel: macChannel)
            var rec = try dev.seal(TunnelHeader(type: .request, flags: TunnelHeader.last, msg: 1, part: 0), payload: Data("hi".utf8))
            tamper(&rec)
            XCTAssertThrowsError(try mac.open(rec), name) { XCTAssertEqual($0 as? TunnelError, .open, name) }
        }
    }

    func testReplayWindow() throws {
        let (dev, mac) = try TunnelTestResponder.pair()
        let recs = try (1...(Int(Tunnel.windowSize) + 20)).map { _ in try dev.seal(TunnelHeader(type: .chunk, flags: TunnelHeader.last, msg: 1, part: 0), payload: Data()) }
        func open(_ counter: Int) throws { _ = try mac.open(recs[counter - 1]) }
        XCTAssertNoThrow(try open(2))
        XCTAssertNoThrow(try open(1))
        XCTAssertThrowsError(try open(1)) { XCTAssertEqual($0 as? TunnelError, .replay) }
        XCTAssertThrowsError(try open(2)) { XCTAssertEqual($0 as? TunnelError, .replay) }
        XCTAssertNoThrow(try open(Int(Tunnel.windowSize) + 10))
        XCTAssertThrowsError(try open(10)) { XCTAssertEqual($0 as? TunnelError, .replay) }
        XCTAssertNoThrow(try open(11))
        XCTAssertNoThrow(try open(Int(Tunnel.windowSize) + 20))
        XCTAssertThrowsError(try open(Int(Tunnel.windowSize) + 10)) { XCTAssertEqual($0 as? TunnelError, .replay) }
    }

    func testSealStopsAtTheRecordLimit() throws {
        let (dev, _) = try TunnelTestResponder.pair()
        dev.send.withLock { $0.counter = Tunnel.maxRecords }
        XCTAssertThrowsError(try dev.seal(TunnelHeader(type: .chunk, flags: TunnelHeader.last, msg: 1, part: 0), payload: Data())) {
            XCTAssertEqual($0 as? TunnelError, .exhausted)
        }
    }
}
