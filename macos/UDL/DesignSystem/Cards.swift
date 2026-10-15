import SwiftUI

/// `.card` — titled container with a sunken header.
struct Card<Content: View, Accessory: View>: View {
    let title: String
    var subtitle: String?
    var flush = false
    @ViewBuilder var accessory: () -> Accessory
    @ViewBuilder var content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).font(Typography.cardTitle)
                    if let subtitle {
                        Text(subtitle)
                            .font(Typography.caption)
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
                Spacer(minLength: 0)
                accessory()
            }
            .padding(.horizontal, 13)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.sunken)
            .overlay(alignment: .bottom) {
                Rectangle().fill(Theme.separator).frame(height: 0.5)
            }

            VStack(alignment: .leading, spacing: 8, content: content)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(flush ? 0 : 13)
        }
        .background(Theme.window)
        .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius))
        .overlay(
            RoundedRectangle(cornerRadius: Metrics.cardRadius)
                .stroke(Theme.separator, lineWidth: 0.5)
        )
    }
}

extension Card where Accessory == EmptyView {
    init(
        title: String,
        subtitle: String? = nil,
        flush: Bool = false,
        @ViewBuilder content: @escaping () -> Content
    ) {
        self.init(title: title, subtitle: subtitle, flush: flush, accessory: { EmptyView() }, content: content)
    }
}

/// `.ins-title` / `.sb-head` — the uppercase section header.
struct SectionHeader: View {
    let title: String

    var body: some View {
        Text(title.uppercased())
            .font(Typography.sectionHeader)
            .tracking(0.55)
            .foregroundStyle(Theme.textTertiary)
    }
}

/// `.field` — the label/value row used throughout the inspector.
struct FieldRow<Value: View>: View {
    let label: String
    @ViewBuilder var value: () -> Value

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(label)
                .font(Typography.control)
                .foregroundStyle(Theme.textSecondary)
            Spacer(minLength: 0)
            value()
                .font(Typography.control)
                .monospacedDigit()
                .multilineTextAlignment(.trailing)
        }
        .frame(minHeight: 20)
    }
}

extension FieldRow where Value == Text {
    init(_ label: String, _ value: String) {
        self.init(label: label) { Text(value) }
    }
}

/// `h2.sec` + `p.lede` — the standard workspace heading block.
struct WorkspaceHeader<Accessory: View>: View {
    let title: String
    var lede: String?
    @ViewBuilder var accessory: () -> Accessory

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(Typography.sectionTitle)
                if let lede {
                    Text(lede)
                        .font(Typography.control)
                        .foregroundStyle(Theme.textSecondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 0)
            accessory()
        }
    }
}

extension WorkspaceHeader where Accessory == EmptyView {
    init(title: String, lede: String? = nil) {
        self.init(title: title, lede: lede) { EmptyView() }
    }
}
