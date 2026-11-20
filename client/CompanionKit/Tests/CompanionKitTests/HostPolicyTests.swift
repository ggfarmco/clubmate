import XCTest

import CompanionKit

final class HostPolicyTests: XCTestCase {
    func testAllowsOnlyTailnetAndOptInLoopback() {
        let cases: [(host: String, tailnet: Bool, loopback: Bool)] = [
            ("100.64.0.1", true, true),
            ("100.127.255.254", true, true),
            ("100.128.0.1", false, false),
            ("100.63.255.255", false, false),
            ("192.168.1.5", false, false),
            ("fd7a:115c:a1e0::1", true, true),
            ("[fd7a:115c:a1e0:ab12::5]", true, true),
            ("fd7a:115c:a1e1::1", false, false),
            ("mac.tail1234.ts.net", true, true),
            ("Mac.Tail1234.TS.NET.", true, true),
            ("ts.net", false, false),
            ("mac.ts.net.evil.com", false, false),
            ("evil.com@x.ts.net", false, false),
            ("evil.com#.ts.net", false, false),
            ("evil.com?.ts.net", false, false),
            ("evil.com/x.ts.net", false, false),
            ("evil%2ecom.ts.net", false, false),
            ("evil.com:80.ts.net", false, false),
            ("-bad.tail.ts.net", false, false),
            ("a..ts.net", false, false),
            ("127.0.0.1", false, true),
            ("::1", false, true),
            ("localhost", false, true),
        ]
        for c in cases {
            XCTAssertEqual(HostPolicy.tailnet.allows(c.host), c.tailnet, c.host)
            XCTAssertEqual(HostPolicy.tailnetAndLoopback.allows(c.host), c.loopback, c.host)
        }
    }

    func testRelayURLMustBeHTTPSToADNSName() {
        let cases: [(url: String, release: Bool, debug: Bool)] = [
            ("https://clubmate-push.exe.xyz", true, true),
            ("https://Relay.Example:8443/base", true, true),
            ("http://relay.example", false, false),
            ("https://relay", false, false),
            ("https://203.0.113.5", false, false),
            ("https://[2001:db8::1]", false, false),
            ("https://user@relay.example", false, false),
            ("https://relay.example?x=1", false, false),
            ("https://-bad.example", false, false),
            ("https://evil%2ecom.example", false, false),
            ("https://relay.123", false, false),
            ("ftp://relay.example", false, false),
            ("http://127.0.0.1:8080", false, true),
            ("http://localhost:8080", false, true),
            ("https://127.0.0.1", false, false),
        ]
        for c in cases {
            let url = URL(string: c.url)!
            XCTAssertEqual(HostPolicy.tailnet.allowsRelay(url), c.release, c.url)
            XCTAssertEqual(HostPolicy.tailnetAndLoopback.allowsRelay(url), c.debug, c.url)
        }
    }
}
