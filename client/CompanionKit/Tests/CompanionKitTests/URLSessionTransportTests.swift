import Foundation
import Synchronization
import XCTest

import CompanionKit

final class URLSessionTransportTests: XCTestCase {
    func testDoesNotFollowRedirects() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RedirectingProtocol.self]
        let transport = URLSessionTransport(configuration: configuration)
        let (_, response) = try await transport.send(URLRequest(url: URL(string: "http://100.64.0.2:47621/v1/hello")!))
        XCTAssertEqual(response.statusCode, 302)
        XCTAssertEqual(RedirectingProtocol.hosts, ["100.64.0.2"])
    }
}

private final class RedirectingProtocol: URLProtocol {
    private static let log = Mutex<[String]>([])

    static var hosts: [String] {
        log.withLock { $0 }
    }

    override class func canInit(with request: URLRequest) -> Bool { true }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let url = request.url!
        Self.log.withLock { $0.append(url.host() ?? "") }
        if url.host() == "evil.example.com" {
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocolDidFinishLoading(self)
            return
        }
        let target = URL(string: "http://evil.example.com/steal")!
        let response = HTTPURLResponse(url: url, statusCode: 302, httpVersion: nil, headerFields: ["Location": target.absoluteString])!
        client?.urlProtocol(self, wasRedirectedTo: URLRequest(url: target), redirectResponse: response)
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
