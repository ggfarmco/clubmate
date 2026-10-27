import AppKit
import os

import GhosttyKit

enum GhosttyCallbacks {
    private static let tickScheduled = OSAllocatedUnfairLock(initialState: false)
    private static let derivedConfig = OSAllocatedUnfairLock<UInt>(initialState: 0)

    static func wakeup() {
        let schedule = tickScheduled.withLock { scheduled in
            guard !scheduled else { return false }
            scheduled = true
            return true
        }
        guard schedule else { return }
        DispatchQueue.main.async {
            tickScheduled.withLock { $0 = false }
            MainActor.assumeIsolated { GhosttyApp.shared.tick() }
        }
    }

    static func action(_ target: ghostty_target_s, _ action: ghostty_action_s) -> Bool {
        switch action.tag {
        case GHOSTTY_ACTION_SET_TITLE, GHOSTTY_ACTION_DESKTOP_NOTIFICATION:
            return true
        case GHOSTTY_ACTION_CONFIG_CHANGE:
            guard target.tag == GHOSTTY_TARGET_APP,
                  let config = action.action.config_change.config,
                  let clone = ghostty_config_clone(config)
            else { return true }
            stash(clone)
            return true
        default:
            return false
        }
    }

    static func takeDerivedConfig() -> ghostty_config_t? {
        ghostty_config_t(bitPattern: derivedConfig.withLock { pending in
            defer { pending = 0 }
            return pending
        })
    }

    static func closeSurface(_ userdata: UnsafeMutableRawPointer?) {
        guard let userdata else { return }
        let view = Unmanaged<TerminalSurfaceView>.fromOpaque(userdata).takeUnretainedValue()
        DispatchQueue.main.async {
            MainActor.assumeIsolated { view.childClosed() }
        }
    }

    static func readClipboard(
        _ userdata: UnsafeMutableRawPointer?,
        _ location: ghostty_clipboard_e,
        _ state: UnsafeMutableRawPointer?,
        _ mimes: UnsafePointer<UnsafePointer<CChar>?>?,
        _ mimesCount: Int,
        _ list: Bool
    ) -> ghostty_clipboard_read_result_e {
        guard let surface = surface(userdata) else { return GHOSTTY_CLIPBOARD_READ_UNSUPPORTED }
        let text = pasteboard(location).string(forType: .string)
        let wantsText = (0..<mimesCount).contains { mimes?[$0].map { String(cString: $0) == "text/plain" } ?? false }
        guard wantsText || list else { return GHOSTTY_CLIPBOARD_READ_UNAVAILABLE }
        let payload = ClipboardPayload(
            contents: wantsText ? [ClipboardItem(mime: "text/plain", data: Data((text ?? "").utf8))] : [],
            available: list && text != nil ? ["text/plain"] : []
        )
        complete(surface, payload, confirmed: false, state: UInt(bitPattern: state))
        return GHOSTTY_CLIPBOARD_READ_STARTED
    }

    static func confirmReadClipboard(
        _ userdata: UnsafeMutableRawPointer?,
        _ confirm: UnsafePointer<ghostty_clipboard_confirm_s>?,
        _ state: UnsafeMutableRawPointer?,
        _ request: ghostty_clipboard_request_e
    ) {
        guard let userdata else { return }
        let view = Unmanaged<TerminalSurfaceView>.fromOpaque(userdata).takeUnretainedValue()
        guard let surface = view.surface else { return }
        guard request == GHOSTTY_CLIPBOARD_REQUEST_PASTE, let confirm = confirm?.pointee else {
            ghostty_surface_deny_clipboard_request(surface, state)
            return
        }
        let payload = ClipboardPayload(confirm)
        let state = UInt(bitPattern: state)
        DispatchQueue.main.async {
            MainActor.assumeIsolated { view.confirmPaste(payload, state: state) }
        }
    }

    static func complete(_ surface: ghostty_surface_t, _ payload: ClipboardPayload, confirmed: Bool, state: UInt) {
        var strings: [UnsafeMutablePointer<CChar>] = []
        var buffers: [UnsafeMutableRawBufferPointer] = []
        defer {
            strings.forEach { Darwin.free($0) }
            buffers.forEach { $0.deallocate() }
        }
        let contents: [ghostty_clipboard_content_s] = payload.contents.compactMap { item in
            guard let mime = strdup(item.mime) else { return nil }
            strings.append(mime)
            let buffer = UnsafeMutableRawBufferPointer.allocate(byteCount: max(item.data.count, 1), alignment: 1)
            buffers.append(buffer)
            _ = item.data.copyBytes(to: buffer)
            return ghostty_clipboard_content_s(mime: mime, data: buffer.baseAddress?.assumingMemoryBound(to: CChar.self), len: item.data.count)
        }
        let available: [UnsafePointer<CChar>?] = payload.available.compactMap { mime in
            guard let pointer = strdup(mime) else { return nil }
            strings.append(pointer)
            return UnsafePointer(pointer)
        }
        contents.withUnsafeBufferPointer { contentsBuffer in
            available.withUnsafeBufferPointer { availableBuffer in
                var completion = ghostty_clipboard_complete_s(
                    contents: contentsBuffer.baseAddress,
                    contents_len: contentsBuffer.count,
                    available: availableBuffer.baseAddress,
                    available_len: availableBuffer.count,
                    confirmed: confirmed,
                    remember: false
                )
                ghostty_surface_complete_clipboard_request(surface, &completion, UnsafeMutableRawPointer(bitPattern: state))
            }
        }
    }

    static func writeClipboard(
        _ location: ghostty_clipboard_e,
        _ content: UnsafePointer<ghostty_clipboard_content_s>?,
        _ count: Int,
        _ confirm: Bool
    ) {
        guard !confirm, let content else { return }
        let text = UnsafeBufferPointer(start: content, count: count).first { item in
            item.mime.map { String(cString: $0) == "text/plain" } ?? false
        }
        guard let text, let data = text.data else { return }
        let string = String(decoding: UnsafeRawBufferPointer(start: data, count: text.len), as: UTF8.self)
        let board = pasteboard(location)
        board.clearContents()
        board.setString(string, forType: .string)
    }

    private static func stash(_ config: ghostty_config_t) {
        let raw = UInt(bitPattern: config)
        let stale = derivedConfig.withLock { pending in
            defer { pending = raw }
            return pending
        }
        if let stale = ghostty_config_t(bitPattern: stale) { ghostty_config_free(stale) }
    }

    private static func surface(_ userdata: UnsafeMutableRawPointer?) -> ghostty_surface_t? {
        guard let userdata else { return nil }
        return Unmanaged<TerminalSurfaceView>.fromOpaque(userdata).takeUnretainedValue().surface
    }

    private static func pasteboard(_ location: ghostty_clipboard_e) -> NSPasteboard {
        guard location == GHOSTTY_CLIPBOARD_SELECTION else { return .general }
        return NSPasteboard(name: .init((Bundle.main.bundleIdentifier ?? "com.clubmate.app") + ".selection"))
    }
}

struct ClipboardItem: Sendable {
    let mime: String
    let data: Data
}

struct ClipboardPayload: Sendable {
    let contents: [ClipboardItem]
    let available: [String]

    var preview: String {
        contents.first { $0.mime == "text/plain" }.map { String(decoding: $0.data, as: UTF8.self) } ?? ""
    }
}

extension ClipboardPayload {
    init(_ confirm: ghostty_clipboard_confirm_s) {
        contents = UnsafeBufferPointer(start: confirm.contents, count: confirm.contents == nil ? 0 : confirm.contents_len).compactMap { item in
            guard let mime = item.mime else { return nil }
            let bytes = item.data.map { Data(UnsafeRawBufferPointer(start: $0, count: item.len)) } ?? Data()
            return ClipboardItem(mime: String(cString: mime), data: bytes)
        }
        available = UnsafeBufferPointer(start: confirm.available, count: confirm.available == nil ? 0 : confirm.available_len).compactMap { $0.map { String(cString: $0) } }
    }
}
