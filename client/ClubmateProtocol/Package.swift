// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ClubmateProtocol",
    platforms: [.iOS("27.0"), .macOS("27.0")],
    products: [
        .library(name: "ClubmateProtocol", targets: ["ClubmateProtocol"])
    ],
    targets: [
        .target(name: "ClubmateProtocol"),
        .testTarget(name: "ClubmateProtocolTests", dependencies: ["ClubmateProtocol"]),
    ]
)
