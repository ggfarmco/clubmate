public enum BlockDetector {
    private static let statuses: Set<Int> = [401, 403, 429, 503]
    private static let textPatterns = [
        "access denied", "zugriff verweigert",
        "just a moment", "challenge-form", "cf-chl",
        "are you a robot", "ich bin kein roboter",
    ]
    private static let captchaPatterns = ["captcha-delivery.com", "g-recaptcha", "h-captcha"]

    public static func blocked(status: Int, title: String, html: String) -> Bool {
        let text = (title + "\n" + html).lowercased()
        if captchaPatterns.contains(where: text.contains) { return true }
        return statuses.contains(status) && textPatterns.contains(where: text.contains)
    }
}
