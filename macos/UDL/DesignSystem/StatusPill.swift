import SwiftUI

/// `.pill` — a compact labelled state marker.
struct StatusPill: View {
    let title: String
    var severity: Severity = .idle
    var showsDot = true

    var body: some View {
        HStack(spacing: 5) {
            if showsDot {
                Circle()
                    .fill(severity.tint)
                    .frame(width: 6, height: 6)
            }
            Text(title)
                .font(Typography.pill)
                .foregroundStyle(severity.tint)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 2)
        .background(severity.background, in: RoundedRectangle(cornerRadius: 5))
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(title), \(severity.rawValue)")
    }
}

/// `.badge` — the numeric sidebar counter.
struct CountBadge: View {
    let count: Int
    var severity: Severity = .idle

    var body: some View {
        Text(count, format: .number)
            .font(Typography.pill)
            .monospacedDigit()
            .foregroundStyle(severity == .idle ? Theme.textSecondary : severity.tint)
            .padding(.horizontal, 6)
            .padding(.vertical, 1)
            .background(
                severity == .idle ? Theme.fill : severity.background,
                in: Capsule()
            )
    }
}
