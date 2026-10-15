import SwiftUI

/// C3 — Go blocks on every `ui.*` request until the app replies. While a
/// prompt is pending the banner says so explicitly, and progress meters render
/// frozen rather than animating, so a stalled run is never mistaken for a
/// working one.
struct WaitingBanner: View {
    var title = "Backend paused — waiting for your selection."
    var detail: String?

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "pause.circle.fill")
                .foregroundStyle(Theme.accent)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(Typography.control)
                    .fontWeight(.semibold)
                if let detail {
                    Text(detail)
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textSecondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.infoBackground)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A determinate meter that visibly freezes while the backend is blocked.
struct FrozenProgressView: View {
    let value: Double
    let total: Double
    var isFrozen = false
    var severity: Severity = .info

    var body: some View {
        ProgressView(value: min(max(value, 0), max(total, 0.0001)), total: max(total, 0.0001))
            .progressViewStyle(.linear)
            .tint(isFrozen ? Theme.idle : severity.tint)
            .opacity(isFrozen ? 0.55 : 1)
    }
}
