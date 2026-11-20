#if DEBUG
import CryptoKit
import Foundation

public enum TunnelResponder {
    public static func respond(channel: String, msg1: Data, statics: [Curve25519.KeyAgreement.PrivateKey], ephemeral: Curve25519.KeyAgreement.PrivateKey = .init(), accept: Int? = nil, onTicketHash: (String?) -> Void = { _ in }) throws -> (TunnelChannel, Data) {
        guard msg1.count >= 1 + Tunnel.keySize + Tunnel.tagSize, msg1.first == Tunnel.version else { throw TunnelError.handshake }
        let devEph = Data(msg1.dropFirst().prefix(Tunnel.keySize))
        for s in statics {
            let pub = s.publicKey.rawRepresentation
            let es = try Tunnel.agree(s, devEph)
            let h0 = Tunnel.hash(Data(Tunnel.protocolName.utf8), pub)
            let h1 = Tunnel.hash(h0, devEph)
            let k1 = Tunnel.hkdf(es, salt: h0, info: Data(Tunnel.infoK1.utf8), count: Tunnel.keySize)
            guard let plain = try? Tunnel.openOnce(k1, aad: h1, Data(msg1.dropFirst(1 + Tunnel.keySize))) else { continue }
            let hello = try JSONDecoder().decode(Hello.self, from: plain)
            guard hello.kid == Tunnel.keyID(pub) else { throw TunnelError.handshake }
            onTicketHash(hello.th)
            let macEph = ephemeral.publicKey.rawRepresentation
            let keys = TunnelKeys(es: es, ee: try Tunnel.agree(ephemeral, devEph), h1: h1, macEph: macEph)
            let dict = accept ?? (hello.dicts.contains(Tunnel.dictV1) ? Tunnel.dictV1 : Tunnel.dictNone)
            let msg2 = macEph + (try Tunnel.sealOnce(keys.k2, aad: keys.h2, Data("{\"dict\":\(dict)}".utf8)))
            return (TunnelChannel(id: channel, dict: dict, keyID: Tunnel.keyID(pub), keys: keys, sendKey: keys.kmd, recvKey: keys.kdm), msg2)
        }
        throw TunnelError.handshake
    }

    private struct Hello: Decodable {
        var dicts: [Int]
        var kid: String
        var th: String?
    }
}
#endif
