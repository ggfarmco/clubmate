import CryptoKit
import Foundation

@testable import CompanionKit

struct TunnelVectors: Decodable {
    struct Bucket: Decodable {
        var size: Int
        var bucket: Int
    }

    struct Pack: Decodable {
        var name: String
        var type: UInt8
        var head: String
        var body: String
        var dict: Int
        var deflate: Bool
        var flags: UInt8
        var plain: String
    }

    struct Record: Decodable {
        var pack: String
        var dir: String
        var counter: UInt64
        var type: UInt8
        var flags: UInt8
        var msg: UInt32
        var re: UInt32?
        var seq: UInt32?
        var part: UInt16
        var payload: String
        var wire: String
    }

    var ticketHash: String
    var macStaticPriv: String
    var macStaticPub: String
    var macKey: String
    var kid: String
    var deviceEphPriv: String
    var deviceEphPub: String
    var macEphPriv: String
    var macEphPub: String
    var channel: String
    var dicts: [Int]
    var dict: Int
    var h0: String
    var h1: String
    var h2: String
    var es: String
    var ee: String
    var k1: String
    var k2: String
    var kDM: String
    var kMD: String
    var msg1: String
    var msg2: String
    var dictV1SHA256: String
    var buckets: [Bucket]
    var packs: [Pack]
    var records: [Record]

    static func load() throws -> TunnelVectors {
        try JSONDecoder().decode(TunnelVectors.self, from: fixture("tunnel-vectors"))
    }

    func key(_ hex: String) throws -> Curve25519.KeyAgreement.PrivateKey {
        try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: Data(hex: hex))
    }
}

extension Data {
    init(hex: String) {
        var d = Data(capacity: hex.count / 2)
        var i = hex.startIndex
        while i < hex.endIndex {
            let j = hex.index(i, offsetBy: 2)
            d.append(UInt8(hex[i..<j], radix: 16)!)
            i = j
        }
        self = d
    }

    var hex: String { map { String(format: "%02x", $0) }.joined() }
}

enum TunnelTestResponder {
    static func respond(channel: String, msg1: Data, statics: [Curve25519.KeyAgreement.PrivateKey], ephemeral: Curve25519.KeyAgreement.PrivateKey = .init(), accept: Int? = nil, onTicketHash: (String?) -> Void = { _ in }) throws -> (TunnelChannel, Data) {
        try TunnelResponder.respond(channel: channel, msg1: msg1, statics: statics, ephemeral: ephemeral, accept: accept, onTicketHash: onTicketHash)
    }

    static func pair(channel: String = "ch_test", macChannel: String? = nil) throws -> (device: TunnelChannel, mac: TunnelChannel) {
        let s = Curve25519.KeyAgreement.PrivateKey()
        let (hs, msg1) = try TunnelHandshake.initiate(macKey: s.publicKey.rawRepresentation)
        let (mac, msg2) = try respond(channel: macChannel ?? channel, msg1: msg1, statics: [s])
        return (try hs.finish(channel: channel, msg2: msg2), mac)
    }
}
