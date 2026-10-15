import SwiftUI

/// `doctor.run`, rendered as a severity-ordered report you can work through:
/// counts that double as filters, categories in the sidebar, and an inspector
/// that turns the selected check into exactly one next action.
struct DoctorView: View {
    @EnvironmentObject private var appState: AppState

    /// The severity tiles are the filter. `nil` means "everything".
    @State private var severityFilter: Severity?
    /// The sidebar's category filter. `nil` means "all categories".
    @State private var category: String?
    @State private var selection: DoctorCheck.ID?

    private static let allCategories = "__all__"

    private var attention: AppState.Attention { appState.attention }

    var body: some View {
        content
            .workspaceToolbar(title: "Check System", subtitle: "doctor.run") {
                Button {
                    Task { await appState.refreshDoctor() }
                } label: {
                    if appState.isLoadingDoctor {
                        Label { Text("Running checks…") } icon: { ProgressView().controlSize(.small) }
                    } else {
                        Label("Re-run checks", systemImage: "arrow.clockwise")
                    }
                }
                .disabled(appState.isLoadingDoctor)
            }
            .workspaceInspector { inspector }
            .udlStatusBar { statusSummary } actions: { statusActions }
            .sidebarContext(sidebarContext)
            .task {
                // The TUI's Check System workflow runs on entry; so does this.
                if appState.doctor == nil, !appState.isLoadingDoctor {
                    await appState.refreshDoctor()
                }
                selectFirstVisible()
            }
            .onChange(of: appState.doctor?.checks.map(\.id) ?? []) { _, _ in
                selectFirstVisible()
            }
    }

    // MARK: Content

    @ViewBuilder private var content: some View {
        VStack(alignment: .leading, spacing: 0) {
            // A failed `doctor.run` used to raise a modal that named no screen.
            // It belongs here, above the report it failed to replace.
            if let status = appState.doctorStatus, status.severity != .info {
                Callout(title: status.message, severity: status.severity)
                    .padding(.horizontal, Metrics.contentPaddingHorizontal)
                    .padding(.top, 10)
            }
            if let report = appState.doctor {
                tiles(report)
                checkList(report)
            } else {
                EmptyStateView(
                    title: "Check System",
                    kind: appState.isLoadingDoctor
                        ? .working
                        : .notRun(action: "run the checks to see the report")
                )
            }
        }
    }

    /// The counts are the filter, exactly as in the mockup: pressing one is the
    /// only way to narrow the list by severity, so the number and the filter
    /// can never disagree.
    private func tiles(_ report: DoctorResult) -> some View {
        let counts = severityCounts(report)
        // The mockup's fourth tile, Info, is absent: `doctor.run` derives every
        // check's status from its severity and spells `info` as `ok`, so an
        // Info tile could only ever read 0. See the inspector note.
        return HStack(spacing: 9) {
            ForEach([Severity.error, .warn, .ok], id: \.self) { severity in
                Button {
                    severityFilter = severityFilter == severity ? nil : severity
                    selectFirstVisible()
                } label: {
                    SeverityTile(
                        severity: severity,
                        title: severity.groupTitle,
                        count: counts[severity] ?? 0,
                        isSelected: severityFilter == severity
                    )
                }
                .buttonStyle(.plain)
                .help(
                    severityFilter == severity
                        ? "Showing only \(severity.groupTitle.lowercased()). Click again to show every check."
                        : "Show only \(severity.groupTitle.lowercased())."
                )
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 14)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }

    @ViewBuilder
    private func checkList(_ report: DoctorResult) -> some View {
        let groups = groupedChecks(report)
        if groups.isEmpty {
            EmptyStateView(title: "Nothing matches", kind: .empty, detail: filterDescription)
        } else {
            List(selection: $selection) {
                ForEach(groups, id: \.severity) { group in
                    Section {
                        ForEach(group.checks) { check in
                            checkRow(check).tag(check.id)
                        }
                    } header: {
                        SectionHeader(title: "\(group.severity.groupTitle) · \(group.checks.count)")
                    }
                }
            }
            .listStyle(.inset)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func checkRow(_ check: DoctorCheck) -> some View {
        HStack(alignment: .center, spacing: 11) {
            SeverityBadge(severity: Severity.forCheck(check))
            VStack(alignment: .leading, spacing: 2) {
                Text(check.detail)
                    .font(Typography.control)
                    .lineLimit(1)
                    .truncationMode(.tail)
                if let remediation = check.remediation, !remediation.isEmpty {
                    Text(remediation)
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textTertiary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            Text(check.name)
                .font(Typography.monoSmall)
                .foregroundStyle(Theme.textTertiary)
        }
        .padding(.vertical, 3)
    }

    // MARK: Sidebar

    /// The category filter lives here rather than in the toolbar, so the
    /// toolbar stays a title plus one action.
    private var sidebarContext: SidebarContext? {
        guard let report = appState.doctor else { return nil }
        var items = [
            SidebarContextItem(
                id: Self.allCategories,
                title: "All categories",
                subtitle: "\(report.checks.count) checks"
            )
        ]
        items += doctorCategories(report).map { name in
            let checks = report.checks.filter { $0.name == name }
            let worst = Severity.worst(checks.map(Severity.forCheck))
            let noun = checks.count == 1 ? "check" : "checks"
            return SidebarContextItem(
                id: name,
                title: name,
                subtitle: worst == .ok
                    ? "\(checks.count) \(noun)"
                    : "\(checks.count) \(noun) · worst \(worst.groupTitle.lowercased())"
            )
        }
        return SidebarContext(
            title: "Categories",
            items: items,
            selectedID: category ?? Self.allCategories,
            note: "udl never installs software for you.",
            select: { id in
                category = id == Self.allCategories ? nil : id
                selectFirstVisible()
            }
        )
    }

    // MARK: Inspector

    @ViewBuilder private var inspector: some View {
        if let report = appState.doctor {
            InspectorSection(title: "Check") {
                if let check = selectedCheck(report) {
                    checkInspector(check, report: report)
                } else {
                    ConstraintNote(text: "Select a check to see its detail and its one next action.")
                }
            }

            InspectorSection(title: "Report") {
                FieldRow("Checks", "\(report.checks.count)")
                FieldRow("Exit code", "\(report.exitCode)")
                ConstraintNote(text: "doctor.run reports no run timestamp, so this screen does not claim one.")
                ConstraintNote(text: "There is no Info tile: doctor.run derives each check's status from its severity and reports info-severity checks as ok, so they are counted as passed.")
            }

            InspectorSection(title: "Effective PATH") {
                PathField(label: "PATH", path: report.effectivePath)
            }

            InspectorSection(title: "Resolved dependencies") {
                if report.resolvedDependencies.isEmpty {
                    ConstraintNote(text: "doctor.run resolved no external dependencies.")
                } else {
                    ForEach(report.resolvedDependencies.sorted(by: { $0.key < $1.key }), id: \.key) { name, path in
                        PathField(label: name, path: path)
                    }
                }
            }
        } else {
            InspectorSection(title: "Report") {
                ConstraintNote(text: "doctor.run has not been called in this session.")
            }
        }
    }

    @ViewBuilder
    private func checkInspector(_ check: DoctorCheck, report: DoctorResult) -> some View {
        let severity = Severity.forCheck(check)
        let fix = DoctorFix(check)

        HStack(spacing: 7) {
            SeverityBadge(severity: severity)
            Text(check.name).font(Typography.cardTitle)
            Spacer(minLength: 0)
        }
        FieldRow(label: "Status") {
            StatusPill(title: check.status, severity: severity, showsDot: false)
        }
        FieldRow("Severity", severity.itemTitle)
        Text(check.detail)
            .font(Typography.caption)
            .foregroundStyle(Theme.textSecondary)
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)

        if let remediation = check.remediation, !remediation.isEmpty {
            ConstraintNote(text: remediation, severity: severity, symbol: "wrench.and.screwdriver")
        }

        if let resolved = doctorResolvedPath(for: check, in: report) {
            PathField(label: resolved.name, path: resolved.path)
        }

        if let destination = fix.destination, let label = fix.actionLabel {
            Button(label) { appState.destination = destination }
                .buttonStyle(.borderedProminent)
                .frame(maxWidth: .infinity)
        } else if severity == .ok {
            ConstraintNote(text: "This check passed. Nothing to do.")
        } else {
            // Failure mode 1: never a disabled button with no stated reason.
            ConstraintNote(
                text: "udl names no screen that fixes this one, so there is no button here. The detail above is the whole of what doctor.run reports.",
                severity: .warn
            )
        }
    }

    // MARK: Status bar

    private var statusSummary: some View {
        SummaryLine {
            if appState.doctor == nil {
                Text("Not run yet in this session")
            } else {
                SummaryCount(value: attention.doctorErrors, noun: "errors", severity: attention.doctorErrors > 0 ? .error : .idle)
                Text("·")
                SummaryCount(value: attention.doctorWarnings, noun: "warnings", severity: attention.doctorWarnings > 0 ? .warn : .idle)
                Text("·")
                SummaryCount(value: attention.doctorPassed, noun: "passed", severity: .ok)
                if severityFilter != nil || category != nil {
                    Text("·")
                    Text(filterDescription).foregroundStyle(Theme.accent)
                }
            }
        }
    }

    @ViewBuilder private var statusActions: some View {
        Button("Open Credentials") { appState.destination = .credentials }
        // The primary label follows the worst severity found, so the button
        // never hides the fact that you would be proceeding over an error.
        Button(primaryLabel) {
            if appState.doctor == nil {
                Task { await appState.refreshDoctor() }
            } else {
                Task { await appState.startDryRunPlan() }
            }
        }
        .buttonStyle(.borderedProminent)
        .constrained(by: appState.isLoadingDoctor
                     ? "doctor.run is still running."
                     : (appState.doctor != nil ? appState.busyReason : nil))
    }

    private var primaryLabel: String {
        guard appState.doctor != nil else { return "Run checks" }
        if attention.doctorErrors > 0 {
            return "Start dry run anyway (\(attention.doctorErrors) unresolved)"
        }
        return "Continue to dry run & plan"
    }

    // MARK: Data

    private func severityCounts(_ report: DoctorResult) -> [Severity: Int] {
        report.checks.reduce(into: [:]) { counts, check in
            counts[Severity.forCheck(check), default: 0] += 1
        }
    }

    private func visibleChecks(_ report: DoctorResult) -> [DoctorCheck] {
        report.checks.filter { check in
            (severityFilter == nil || Severity.forCheck(check) == severityFilter)
                && (category == nil || check.name == category)
        }
    }

    private func groupedChecks(_ report: DoctorResult) -> [(severity: Severity, checks: [DoctorCheck])] {
        let visible = visibleChecks(report)
        return doctorSeverityOrder.compactMap { severity in
            let checks = visible.filter { Severity.forCheck($0) == severity }
            return checks.isEmpty ? nil : (severity, checks)
        }
    }

    private func selectedCheck(_ report: DoctorResult) -> DoctorCheck? {
        guard let selection else { return nil }
        return report.checks.first { $0.id == selection }
    }

    private var filterDescription: String {
        switch (severityFilter, category) {
        case (nil, nil): "Showing every check."
        case (let severity?, nil): "Filtered to \(severity.groupTitle.lowercased())."
        case (nil, let name?): "Filtered to \(name)."
        case (let severity?, let name?): "Filtered to \(severity.groupTitle.lowercased()) in \(name)."
        }
    }

    private func selectFirstVisible() {
        guard let report = appState.doctor else {
            selection = nil
            return
        }
        if let selection, visibleChecks(report).contains(where: { $0.id == selection }) { return }
        selection = groupedChecks(report).first?.checks.first?.id
    }
}
