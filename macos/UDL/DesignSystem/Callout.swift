import SwiftUI

/// `.callout` — an inline explanation attached to a screen rather than a
/// control. Always states a reason; never a bare colored strip.
struct Callout<Actions: View>: View {
    let title: String
    var detail: String?
    var severity: Severity = .info
    @ViewBuilder var actions: () -> Actions

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: severity.symbolName)
                .foregroundStyle(severity.tint)
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(Typography.control)
                    .fontWeight(.semibold)
                    .fixedSize(horizontal: false, vertical: true)
                if let detail {
                    Text(detail)
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textSecondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 0)
            actions()
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(severity.background, in: RoundedRectangle(cornerRadius: Metrics.cardRadius))
    }
}

extension Callout where Actions == EmptyView {
    init(title: String, detail: String? = nil, severity: Severity = .info) {
        self.init(title: title, detail: detail, severity: severity) { EmptyView() }
    }
}
