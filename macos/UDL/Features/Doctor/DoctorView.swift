import SwiftUI

enum DoctorPresentationState: Equatable {
    case healthy
    case warning
    case blocked
}

func doctorPresentationState(_ report: DoctorResult) -> DoctorPresentationState {
    if report.checks.contains(where: { $0.status == "blocked" }) {
        return .blocked
    }
    if report.exitCode != 0 || report.checks.contains(where: { $0.status == "warning" }) {
        return .warning
    }
    return .healthy
}

struct DoctorView: View {
    @EnvironmentObject private var appState: AppState
    @State private var pathExpanded = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 26) {
                header
                if let report = appState.doctor {
                    summary(report)
                    LazyVStack(spacing: 10) {
                        ForEach(report.checks) { check in
                            checkRow(check)
                        }
                    }
                    environment(report)
                } else if appState.isLoadingDoctor {
                    ProgressView("Running system checks…")
                        .controlSize(.large)
                        .frame(maxWidth: .infinity, minHeight: 260)
                } else {
                    ContentUnavailableView(
                        "Doctor is waiting",
                        systemImage: "stethoscope",
                        description: Text("Connect the backend, then run the checks again.")
                    )
                }
            }
            .padding(34)
            .frame(maxWidth: 1040, alignment: .leading)
        }
        .refreshable { await appState.refreshDoctor() }
    }

    private var header: some View {
        HStack(alignment: .bottom) {
            VStack(alignment: .leading, spacing: 7) {
                Text("SYSTEM DOCTOR")
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .tracking(1.8)
                    .foregroundStyle(.orange)
                Text("Signal check")
                    .font(.system(size: 36, weight: .heavy, design: .rounded))
                Text("Backend, dependencies, credentials, storage, and Rekordbox readiness.")
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button {
                Task { await appState.refreshDoctor() }
            } label: {
                Label("Run again", systemImage: "arrow.clockwise")
            }
            .buttonStyle(.borderedProminent)
            .disabled(appState.isLoadingDoctor)
        }
    }

    private func summary(_ report: DoctorResult) -> some View {
        let blocked = report.checks.filter { $0.status == "blocked" }.count
        let warnings = report.checks.filter { $0.status == "warning" }.count
        return HStack(spacing: 14) {
            metric("Checks", "\(report.checks.count)", .white)
            metric("Warnings", "\(warnings)", .orange)
            metric("Blocked", "\(blocked)", .red)
            Spacer()
            Text(report.exitCode == 0 ? "READY" : "ATTENTION")
                .font(.system(size: 12, weight: .black, design: .monospaced))
                .tracking(1.4)
                .foregroundStyle(report.exitCode == 0 ? .green : .orange)
        }
        .padding(18)
        .background(.white.opacity(0.045), in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).stroke(.white.opacity(0.08)))
    }

    private func metric(_ label: String, _ value: String, _ color: Color) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(value).font(.title2.bold()).foregroundStyle(color)
            Text(label).font(.caption.monospaced()).foregroundStyle(.secondary)
        }
        .frame(minWidth: 80, alignment: .leading)
    }

    private func checkRow(_ check: DoctorCheck) -> some View {
        HStack(alignment: .top, spacing: 14) {
            Image(systemName: icon(check))
                .foregroundStyle(color(check))
                .font(.title3)
                .frame(width: 26)
            VStack(alignment: .leading, spacing: 5) {
                HStack {
                    Text(check.name.uppercased())
                        .font(.caption.bold().monospaced())
                        .foregroundStyle(.secondary)
                    Text(check.status.uppercased())
                        .font(.caption2.bold().monospaced())
                        .foregroundStyle(color(check))
                }
                Text(check.detail).textSelection(.enabled)
                if let remediation = check.remediation, !remediation.isEmpty {
                    Text(remediation)
                        .font(.callout)
                        .foregroundStyle(.orange)
                }
            }
            Spacer()
        }
        .padding(16)
        .background(.white.opacity(0.035), in: RoundedRectangle(cornerRadius: 12))
    }

    private func environment(_ report: DoctorResult) -> some View {
        DisclosureGroup("Backend environment", isExpanded: $pathExpanded) {
            VStack(alignment: .leading, spacing: 12) {
                Text(report.effectivePath)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                ForEach(report.resolvedDependencies.sorted(by: { $0.key < $1.key }), id: \.key) { name, path in
                    LabeledContent(name, value: path)
                        .font(.caption.monospaced())
                        .textSelection(.enabled)
                }
            }
            .padding(.top, 10)
        }
        .padding(16)
        .background(.white.opacity(0.025), in: RoundedRectangle(cornerRadius: 12))
    }

    private func color(_ check: DoctorCheck) -> Color {
        switch check.status {
        case "blocked": .red
        case "warning": .orange
        default: .green
        }
    }

    private func icon(_ check: DoctorCheck) -> String {
        switch check.status {
        case "blocked": "xmark.octagon.fill"
        case "warning": "exclamationmark.triangle.fill"
        default: "checkmark.circle.fill"
        }
    }
}
