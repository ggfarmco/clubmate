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
        let contents = wantsText ? Data((text ?? "").utf8) : nil
        complete(surface, text: contents, available: list && text != nil, state: state)
        return GHOSTTY_CLIPBOARD_READ_STARTED
    }

    static func confirmReadClipboard(
        _ userdata: UnsafeMutableRawPointer?,
        _ confirm: UnsafePointer<ghostty_clipboard_confirm_s>?,
        _ state: UnsafeMutableRawPointer?,
        _ request: ghostty_clipboard_request_e
    ) {
        guard let surface = surface(userdata) else { return }
        guard request == GHOSTTY_CLIPBOARD_REQUEST_PASTE, let confirm = confirm?.pointee else {
            ghostty_surface_deny_clipboard_request(surface, state)
            return
        }
        var completion = ghostty_clipboard_complete_s(
            contents: confirm.contents,
            contents_len: confirm.contents_len,
            available: confirm.available,
            available_len: confirm.available_len,
            confirmed: true,
            remember: false
        )
        ghostty_surface_complete_clipboard_request(surface, &completion, state)
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

    private static func complete(_ surface: ghostty_surface_t, text: Data?, available: Bool, state: UnsafeMutableRawPointer?) {
        let bytes = Array(text ?? Data()).map { CChar(bitPattern: $0) } + [0]
        "text/plain".withCString { mimePointer in
            bytes.withUnsafeBufferPointer { buffer in
                var content = ghostty_clipboard_content_s(
                    mime: mimePointer,
                    data: buffer.baseAddress,
                    len: buffer.count - 1
                )
                var availableMime: UnsafePointer<CChar>? = mimePointer
                withUnsafePointer(to: &content) { contentPointer in
                    withUnsafePointer(to: &availableMime) { availablePointer in
                        var completion = ghostty_clipboard_complete_s(
                            contents: text == nil ? nil : contentPointer,
                            contents_len: text == nil ? 0 : 1,
                            available: available ? availablePointer : nil,
                            available_len: available ? 1 : 0,
                            confirmed: false,
                            remember: false
                        )
                        ghostty_surface_complete_clipboard_request(surface, &completion, state)
                    }
                }
            }
        }
    }
}
