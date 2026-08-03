import SwiftUI

/// `.sev` — the filled severity bubble used in lists and section headers.
struct SeverityBadge: View {
    let severity: Severity
    var size: CGFloat = 16

    var body: some View {
        ZStack {
            Circle().fill(severity.tint)
            Image(systemName: severity.badgeSymbol)
                .font(.system(size: size * 0.55, weight: .black))
                .foregroundStyle(.white)
        }
        .frame(width: size, height: size)
        .accessibilityLabel(severity.rawValue)
    }
}

/// A severity count that doubles as a filter tile on Doctor and Home.
struct SeverityTile: View {
    let severity: Severity
    let title: String
    let count: Int
    var isSelected = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                SeverityBadge(severity: severity)
                Text(title)
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textSecondary)
            }
            Text(count, format: .number)
                .font(Typography.hero)
                .monospacedDigit()
                .foregroundStyle(count == 0 ? Theme.textTertiary : severity.tint)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(Theme.sunken, in: RoundedRectangle(cornerRadius: Metrics.cardRadius))
        .overlay(
            RoundedRectangle(cornerRadius: Metrics.cardRadius)
                .stroke(isSelected ? Theme.accent : Theme.separator, lineWidth: isSelected ? 2 : 0.5)
        )
    }
}
