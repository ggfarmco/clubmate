public enum ColorSchemeChoice {
    public static func isDark(appearanceName: String) -> Bool {
        appearanceName.contains("Dark")
    }
}
