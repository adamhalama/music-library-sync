import AppKit
import SwiftUI

/// `.path` — a labelled, selectable, copyable filesystem path. Paths shown to
/// the user always come from the protocol (`session.initialize`,
/// `PlanSourceDetails`, `config.readFile`), never from a guess.
struct PathField: View {
    let label: String
    let path: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label.uppercased())
                .font(Typography.sectionHeader)
                .tracking(0.4)
                .foregroundStyle(Theme.textTertiary)
            Text(path)
                .font(Typography.monoSmall)
                .foregroundStyle(Theme.textSecondary)
                .textSelection(.enabled)
                .lineLimit(3)
                .truncationMode(.middle)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 7)
        .padding(.vertical, 5)
        .background(Theme.window, in: RoundedRectangle(cornerRadius: 5))
        .overlay(
            RoundedRectangle(cornerRadius: 5).stroke(Theme.separator, lineWidth: 0.5)
        )
        .contextMenu {
            Button("Copy Path") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(path, forType: .string)
            }
            Button("Reveal in Finder") {
                NSWorkspace.shared.selectFile(path, inFileViewerRootedAtPath: "")
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(label): \(path)")
    }
}

/// A monospace inline value, for checksums, run IDs, and remote IDs.
struct MonoValue: View {
    let text: String
    var severity: Severity = .idle

    var body: some View {
        Text(text)
            .font(Typography.mono)
            .foregroundStyle(severity == .idle ? Theme.textSecondary : severity.tint)
            .textSelection(.enabled)
            .truncationMode(.middle)
            .lineLimit(1)
    }
}
