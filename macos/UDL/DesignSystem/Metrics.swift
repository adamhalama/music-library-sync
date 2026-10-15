import SwiftUI

/// Fixed dimensions taken from `docs/gui-redesign/app/shell.css`.
enum Metrics {
    static let sidebarWidth: CGFloat = 216
    static let sidebarMinWidth: CGFloat = 190
    static let sidebarMaxWidth: CGFloat = 280
    static let inspectorWidth: CGFloat = 266
    static let inspectorMinWidth: CGFloat = 240
    static let inspectorMaxWidth: CGFloat = 340
    static let statusBarHeight: CGFloat = 46
    static let toolbarHeight: CGFloat = 52
    static let tableRowHeight: CGFloat = 26
    static let listRowHeight: CGFloat = 44

    static let cardRadius: CGFloat = 9
    static let windowRadius: CGFloat = 11
    static let controlRadius: CGFloat = 6

    /// `.pad` — the standard content inset.
    static let contentPadding: CGFloat = 18
    static let contentPaddingHorizontal: CGFloat = 22
    static let gutter: CGFloat = 12
    static let tightGutter: CGFloat = 6
}

/// Every font size in the application. No view outside `DesignSystem/` sets one.
enum Typography {
    /// `.hero h1`
    static let hero = Font.system(size: 22, weight: .bold)
    /// `h2.sec`
    static let sectionTitle = Font.system(size: 15, weight: .semibold)
    /// `.card-title`, `.wcard h4`
    static let cardTitle = Font.system(size: 13, weight: .semibold)
    /// body copy
    static let body = Font.system(size: 13)
    /// `.summary`, `.field`, table rows
    static let control = Font.system(size: 12)
    /// `.hint`, `.tb-sub`
    static let caption = Font.system(size: 11)
    /// `.ins-title`, `.sb-head`
    static let sectionHeader = Font.system(size: 11, weight: .semibold)
    /// `.pill`, `.badge`
    static let pill = Font.system(size: 11, weight: .medium)

    static let mono = Font.system(size: 11, design: .monospaced)
    static let monoSmall = Font.system(size: 10.5, design: .monospaced)
    static let monoBody = Font.system(size: 12, design: .monospaced)
}
