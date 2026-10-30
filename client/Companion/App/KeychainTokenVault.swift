import Foundation
import Security

import CompanionKit

struct KeychainTokenVault: TokenVault {
    enum Failure: LocalizedError {
        case status(OSStatus)

        var errorDescription: String? {
            switch self {
            case .status(let status):
                let text = SecCopyErrorMessageString(status, nil) as String? ?? "unknown error"
                return "Couldn't store the access token in the Keychain (\(status): \(text))."
            }
        }
    }

    static let service = "com.clubmate.companion.token"

    func token(for computerID: String) -> String? {
        var query = base(computerID)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let data = item as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    func setToken(_ token: String, for computerID: String) throws {
        let data = Data(token.utf8)
        let update: [String: Any] = [kSecValueData as String: data, kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly]
        let status = SecItemUpdate(base(computerID) as CFDictionary, update as CFDictionary)
        if status == errSecSuccess { return }
        guard status == errSecItemNotFound else { throw Failure.status(status) }
        var add = base(computerID)
        add.merge(update) { $1 }
        let added = SecItemAdd(add as CFDictionary, nil)
        guard added == errSecSuccess else { throw Failure.status(added) }
    }

    func removeToken(for computerID: String) throws {
        let status = SecItemDelete(base(computerID) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw Failure.status(status) }
    }

    private func base(_ computerID: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: Self.service, kSecAttrAccount as String: computerID]
    }
}
