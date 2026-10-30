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
}
