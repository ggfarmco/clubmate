// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "CompanionKit",
    platforms: [.iOS("27.0"), .macOS("26.0")],
    products: [
        .library(name: "CompanionKit", targets: ["CompanionKit"])
    ],
    dependencies: [
        .package(path: "../ClubmateProtocol")
    ],
    targets: [
        .systemLibrary(name: "CZlib", path: "Sources/CZlib"),
        .target(name: "CompanionKit", dependencies: ["ClubmateProtocol", "CZlib"], resources: [.copy("Resources/dict-v1.bin")]),
        .testTarget(name: "CompanionKitTests", dependencies: ["CompanionKit"], resources: [.copy("Fixtures")]),
    ]
)
