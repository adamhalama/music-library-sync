import SwiftUI

/// C9/C10 — the plan-first, fail-closed Rekordbox workflow.
///
/// Layout follows the mockup: a runtime health strip, then the setup the plan
/// was built from, then the checksummed plan itself as a table with its
/// blockers stated inline. The plan is an opaque `JSONValue` that
/// `rekordbox.apply` receives verbatim; everything on screen is a *read* of it.
struct RekordboxView: View {
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var chrome: ShellChrome

    @State private var target = RekordboxPlanTarget.configDefault
    @State private var rowFilter = RekordboxRowFilter.all
    @State private var showingConfig = false
    @State private var confirmingApply = false

    var body: some View {
        content
            .workspaceToolbar(
                title: "Rekordbox Sync",
                subtitle: "rekordbox.plan · rekordbox.apply",
                searchable: true,
                searchPrompt: "Filter by artist, title or path"
            ) {
                if appState.rekordboxRunID != nil {
                    Button("Cancel", role: .destructive) {
                        Task { await appState.cancelRekordboxOperation() }
                    }
                } else {
                    Button {
                        generatePlan()
                    } label: {
                        Label(appState.rekordboxPlan == nil ? "Generate plan" : "Regenerate plan", systemImage: "arrow.clockwise")
                    }
                    .help("Planning is read-only. It re-reads Music.app and the Rekordbox collection and writes nothing.")
                }
            }
            .workspaceInspector { inspector }
            .udlStatusBar { statusSummary } actions: { statusActions }
            .sidebarContext(sidebarContext)
            .task {
                if appState.rekordboxConfig == nil || appState.rekordboxRuntime == nil {
                    await appState.loadRekordbox()
                }
            }
            .onDisappear { chrome.searchText = "" }
            .sheet(isPresented: $showingConfig) {
                if let config = appState.rekordboxConfig?.config {
                    RekordboxConfigEditor(config: config)
                        .environmentObject(appState)
                }
            }
            .confirmationDialog(
                "Apply this checksummed plan?",
                isPresented: $confirmingApply,
                titleVisibility: .visible
            ) {
                Button("Back up and apply", role: .destructive) {
                    Task { await appState.applyRekordbox(dryRun: false) }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("udl re-verifies the plan checksum, refuses if Rekordbox is running, creates a timestamped database backup, then verifies the result. Do not open Rekordbox until it finishes.")
            }
    }

    // MARK: Content

    private var content: some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 0) {
                if let notResumed = appState.notResumedMessage(for: .rekordbox) {
                    Callout(title: notResumed, severity: .warn)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.top, 12)
                }
                // C10 — a refusal names its blocker here, next to the plan it
                // refused, rather than in a modal stacked over it.
                if let status = appState.rekordboxStatus, status.severity != .info {
                    Callout(title: status.message, severity: status.severity)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.top, 12)
                }
                runtimeStrip
                setupStrip
                planBar
                planBody
            }
        }
    }

    /// `rekordbox.deps.status` as one line, not a card: the managed runtime is
    /// a precondition, and a precondition that is healthy should not occupy a
    /// third of the screen.
    private var runtimeStrip: some View {
        HStack(spacing: 10) {
            if let runtime = appState.rekordboxRuntime?.status {
                SeverityBadge(severity: runtime.healthy ? .ok : .warn)
                VStack(alignment: .leading, spacing: 1) {
                    Text(runtime.message).font(Typography.control).lineLimit(1)
                    Text("\(runtime.pythonBin) · pyrekordbox \(runtime.version ?? "not verified")")
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textTertiary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            } else {
                ProgressView().controlSize(.small)
                Text("Reading runtime status…").font(Typography.control)
            }
            Spacer(minLength: 12)
            Button("Ensure") { Task { await appState.ensureRekordboxRuntime() } }
                .disabled(appState.rekordboxRunID != nil)
            Button("Reset…", role: .destructive) { Task { await appState.resetRekordboxRuntime() } }
                .disabled(appState.rekordboxRunID != nil)
            // One reason for the strip rather than one per button.
            if let busy = appState.busyReason, appState.rekordboxRunID != nil {
                ConstraintNote(text: busy)
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.separator).frame(height: 0.5) }
    }

    /// What the plan is built from: the configured database and the read-only
    /// inspection, both with their own action.
    private var setupStrip: some View {
        HStack(spacing: 14) {
            VStack(alignment: .leading, spacing: 2) {
                SectionHeader(title: "Database")
                if let config = appState.rekordboxConfig {
                    Text(config.config.defaults.dbDir)
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textSecondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                } else {
                    Text("rekordbox.config.read has not returned yet.")
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textTertiary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            VStack(alignment: .leading, spacing: 2) {
                SectionHeader(title: "Inspection")
                if let inspect = appState.rekordboxInspect {
                    Text("\(inspect.playlists.count) playlists · \(inspect.contents.count) tracks")
                        .font(Typography.control)
                } else {
                    Text(isRunning(.inspect) ? "Reading the collection…" : "Not run yet")
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textTertiary)
                }
            }
            .frame(width: 200, alignment: .leading)

            Button("Inspect database") { Task { await appState.inspectRekordbox() } }
                .disabled(appState.rekordboxRunID != nil)
            Button("Edit paths & mappings…") { showingConfig = true }
                .constrained(by: appState.rekordboxConfig == nil
                             ? "rekordbox.config.read has not returned yet."
                             : nil)
            if let busy = appState.busyReason, appState.rekordboxRunID != nil {
                ConstraintNote(text: busy)
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.separator).frame(height: 0.5) }
    }

    /// The mockup's `.planbar`: the apply gate, what the plan targets, and the
    /// checksum that binds it.
    private var planBar: some View {
        HStack(spacing: 12) {
            StatusPill(title: gate.pillTitle, severity: gate.severity)
            VStack(alignment: .leading, spacing: 2) {
                Text(appState.rekordboxPlan?.title ?? "No plan in this session")
                    .font(Typography.cardTitle)
                    .lineLimit(1)
                if let plan = appState.rekordboxPlan {
                    // C9 — the checksum is part of the plan's identity, so it
                    // is stated next to it and not only in the inspector.
                    Text("plan \(plan.version) · generated \(plan.generatedAt) · checksum \(plan.shortChecksum)")
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textTertiary)
                        .textSelection(.enabled)
                        .lineLimit(1)
                } else {
                    Text(isRunning(.plan) ? "reading Music.app and the Rekordbox collection…" : "rekordbox.plan has not been called")
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textTertiary)
                }
            }
            Spacer(minLength: 12)
            Picker("Rows", selection: $rowFilter) {
                ForEach(RekordboxRowFilter.allCases) { option in
                    Text("\(option.label) \(count(of: option))").tag(option)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(width: 250)
            .constrained(by: appState.rekordboxPlan == nil ? "Generate a plan to filter its rows." : nil)
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.sunken)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.separator).frame(height: 0.5) }
    }

    @ViewBuilder private var planBody: some View {
        if let plan = appState.rekordboxPlan {
            VStack(alignment: .leading, spacing: 0) {
                planTable(plan)
                if let obstacle = appState.rekordboxObstacle {
                    obstacleCallout(obstacle)
                } else if !plan.blockers.isEmpty {
                    // C10 — fail-closed: the whole mirror stays visible and no
                    // partial apply is ever sent.
                    Callout(
                        title: "\(plan.blockers.count) \(plan.blockers.count == 1 ? "row blocks" : "rows block") apply.",
                        detail: "udl refuses a partial mirror. The complete plan stays visible; resolve every missing and ambiguous row, then regenerate the plan.",
                        severity: .error
                    )
                    .padding(.horizontal, Metrics.contentPaddingHorizontal)
                    .padding(.vertical, 10)
                }
            }
        } else {
            EmptyStateView(
                title: "Checksummed mirror plan",
                kind: isRunning(.plan)
                    ? .working
                    : .notRun(action: "generate a plan to see every intended playlist change")
            )
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func planTable(_ plan: RekordboxPlanPresentation) -> some View {
        Table(rows(plan)) {
            TableColumn("#") { row in
                Text(row.index, format: .number)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            .width(38)
            TableColumn("Artist") { row in
                Text(row.artist.isEmpty ? "—" : row.artist)
                    .font(Typography.control)
                    .foregroundStyle(row.isBlocked ? Theme.textTertiary : Theme.text)
                    .lineLimit(1)
            }
            .width(min: 110, ideal: 132)
            TableColumn("Track / resolved path") { row in
                VStack(alignment: .leading, spacing: 1) {
                    Text(row.title)
                        .font(Typography.control)
                        .foregroundStyle(row.isBlocked ? Theme.textTertiary : Theme.text)
                        .lineLimit(1)
                    Text(row.path.isEmpty ? "no resolved path" : row.path)
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textTertiary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            TableColumn("Time") { row in
                Text(row.durationLabel)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            .width(58)
            TableColumn("Match") { row in
                StatusPill(title: row.matchLabel, severity: row.matchSeverity)
            }
            .width(min: 104, ideal: 122)
            TableColumn("Action") { row in
                Text(row.actionLabel)
                    .font(Typography.control)
                    .foregroundStyle(row.isBlocked ? Theme.error : Theme.textSecondary)
            }
            .width(72)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
    }

    private func obstacleCallout(_ obstacle: RekordboxObstacle) -> some View {
        Callout(
            title: obstacleTitle(obstacle),
            detail: obstacle.message,
            severity: .error
        ) {
            if obstacle.requiresRegeneration {
                Button("Regenerate plan") { generatePlan() }
                    .buttonStyle(.borderedProminent)
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 10)
    }

    private func obstacleTitle(_ obstacle: RekordboxObstacle) -> String {
        switch obstacle {
        case .rekordboxRunning: "Rekordbox is open, so udl refused."
        case .planDrift: "The plan no longer matches its checksum."
        case .partialMirrorRefused: "udl refuses to apply a partial mirror."
        case .other: "The backend refused this step."
        }
    }

    // MARK: Sidebar

    /// Every value `rekordbox.plan` will accept as a target, so choosing one is
    /// navigation rather than two pickers buried in the content column.
    private var sidebarContext: SidebarContext? {
        guard let config = appState.rekordboxConfig?.config else { return nil }
        var items = [
            SidebarContextItem(
                id: RekordboxPlanTarget.configDefault.id,
                title: "Config default",
                subtitle: "no job_id or mapping_id"
            )
        ]
        items += config.sync.folders.map { mapping in
            SidebarContextItem(
                id: RekordboxPlanTarget.mapping(mapping.id).id,
                title: mapping.id,
                subtitle: "folder → \(mapping.rekordboxFolder ?? mapping.rekordboxFolderID ?? "—")",
                sourceType: "rekordbox"
            )
        }
        items += config.sync.jobs.map { job in
            SidebarContextItem(
                id: RekordboxPlanTarget.job(job.id).id,
                title: job.id,
                subtitle: "playlist → \(job.rekordboxPlaylist ?? job.rekordboxPlaylistID ?? "—")",
                sourceType: "rekordbox"
            )
        }
        return SidebarContext(
            title: "Plan targets",
            items: items,
            selectedID: target.id,
            note: "Planning is read-only. Apply verifies the inputs again and writes only after a timestamped backup succeeds.",
            select: { id in
                guard let selected = RekordboxPlanTarget(id: id), selected.id != target.id else { return }
                target = selected
            }
        )
    }

    // MARK: Inspector

    @ViewBuilder private var inspector: some View {
        if let plan = appState.rekordboxPlan {
            InspectorSection(title: "Plan summary") {
                HStack(spacing: 6) {
                    countTile(plan.willAdd, "Add", severity: .info)
                    countTile(plan.willMove, "Move", severity: .info)
                    countTile(plan.blockers.count, "Blocked", severity: plan.blockers.isEmpty ? .idle : .error)
                }
                FieldRow("Rows", "\(plan.rows.count)")
                FieldRow("Matched", "\(plan.matched)")
                FieldRow("Missing", "\(plan.missing)")
                FieldRow("Ambiguous", "\(plan.ambiguous)")
            }

            InspectorSection(title: "Checksum") {
                // C9 — the plan travels back byte-for-byte.
                PathField(label: "checksum_sha256", path: plan.checksum.isEmpty ? "not reported" : plan.checksum)
                FieldRow("Version", plan.version.isEmpty ? "—" : plan.version)
                ConstraintNote(
                    text: "udl never decodes and re-encodes this plan; rekordbox.apply receives the exact value rekordbox.plan sent. If the checksum drifts, regenerate the plan rather than applying part of it."
                )
            }

            InspectorSection(title: "Apply preconditions") {
                precondition(
                    "Plan carries a checksum",
                    passed: !plan.checksum.isEmpty,
                    detail: plan.checksum.isEmpty
                        ? "The plan result has no checksum_sha256, so apply would be refused."
                        : "Verified again by the backend immediately before the transaction."
                )
                precondition(
                    "Complete mirror",
                    passed: plan.blockers.isEmpty,
                    detail: plan.blockers.isEmpty
                        ? "No missing, ambiguous or duplicate row."
                        : "\(plan.blockers.count) blocked row(s); v1 refuses a partial mirror."
                )
                unknownPrecondition(
                    "Rekordbox is closed",
                    obstacle: appState.rekordboxObstacle,
                    matches: { if case .rekordboxRunning = $0 { true } else { false } },
                    unknownDetail: "The protocol reports no process state. udl checks this when inspect or apply runs, and refuses there."
                )
                unknownPrecondition(
                    "Database unchanged since planning",
                    obstacle: appState.rekordboxObstacle,
                    matches: { if case .planDrift = $0 { true } else { false } },
                    unknownDetail: "Checked by the backend at apply time; nothing before then reports it."
                )
                ConstraintNote(
                    text: "Two of these have no pre-flight source in the protocol. They are shown as unknown rather than as a green tick this app cannot earn."
                )
            }

            InspectorSection(title: "Paths") {
                PathField(label: "Rekordbox database", path: plan.dbDir)
                PathField(label: "Backup", path: plan.backupDir)
            }

            if !plan.planWarnings.isEmpty {
                InspectorSection(title: "Plan warnings") {
                    ForEach(Array(plan.planWarnings.enumerated()), id: \.offset) { _, warning in
                        ConstraintNote(text: warning, severity: .warn, symbol: "exclamationmark.triangle")
                    }
                }
            }
        } else {
            InspectorSection(title: "Plan") {
                ConstraintNote(text: "rekordbox.plan has not been called in this session. There is nothing to check a checksum against.")
            }
        }

        if let status = appState.rekordboxStatus {
            InspectorSection(title: "Last message") {
                Text(status.message)
                    .font(Typography.caption)
                    .foregroundStyle(status.severity == .info ? Theme.textSecondary : status.severity.tint)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }

        if let runtime = appState.rekordboxRuntime?.status {
            InspectorSection(title: "Runtime") {
                FieldRow("Managed", runtime.managed ? "Yes" : "No")
                FieldRow("Installed", runtime.installed ? "Yes" : "No")
                FieldRow("Healthy", runtime.healthy ? "Yes" : "No")
                PathField(label: "Python", path: runtime.pythonBin)
                if let venv = runtime.venvDir {
                    PathField(label: "Virtualenv", path: venv)
                }
            }
        }
    }

    private func countTile(_ value: Int, _ label: String, severity: Severity) -> some View {
        VStack(spacing: 1) {
            Text(value, format: .number)
                .font(Typography.sectionTitle)
                .monospacedDigit()
                .foregroundStyle(severity == .idle ? Theme.text : severity.tint)
            Text(label.uppercased())
                .font(Typography.sectionHeader)
                .tracking(0.4)
                .foregroundStyle(Theme.textTertiary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 7)
        .background(Theme.window, in: RoundedRectangle(cornerRadius: 7))
        .overlay(RoundedRectangle(cornerRadius: 7).stroke(Theme.separator, lineWidth: 0.5))
    }

    private func precondition(_ title: String, passed: Bool, detail: String) -> some View {
        HStack(alignment: .top, spacing: 8) {
            SeverityBadge(severity: passed ? .ok : .error)
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(Typography.control)
                Text(detail)
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
    }

    /// A precondition the protocol cannot report until a run attempts it. It
    /// stays visibly unknown until a real backend refusal names it.
    private func unknownPrecondition(
        _ title: String,
        obstacle: RekordboxObstacle?,
        matches: (RekordboxObstacle) -> Bool,
        unknownDetail: String
    ) -> some View {
        let failed = obstacle.map(matches) ?? false
        return HStack(alignment: .top, spacing: 8) {
            if failed {
                SeverityBadge(severity: .error)
            } else {
                Image(systemName: "questionmark.circle")
                    .foregroundStyle(Theme.textTertiary)
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(Typography.control)
                Text(failed ? (obstacle?.message ?? unknownDetail) : "Unknown — \(unknownDetail)")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
    }

    // MARK: Status bar

    private var statusSummary: some View {
        SummaryLine {
            if let plan = appState.rekordboxPlan {
                SummaryCount(value: plan.rows.count, noun: "rows", severity: .idle)
                Text("·")
                SummaryCount(value: count(of: .changes), noun: "changed rows", severity: .ok)
                Text("·")
                SummaryCount(value: plan.blockers.count, noun: "blocked", severity: plan.blockers.isEmpty ? .idle : .error)
                if plan.willRemove > 0 {
                    // A removal has no music row to show, so it can never
                    // appear in the table. Counting it as a "row" would make
                    // the table and the summary disagree.
                    Text("·")
                    SummaryCount(value: plan.willRemove, noun: "removals with no music row", severity: .warn)
                }
                if rowFilter != .all {
                    Text("·")
                    Text("Filtered to \(rowFilter.label.lowercased()).").foregroundStyle(Theme.accent)
                }
            } else {
                Text("No plan generated in this session")
            }
        }
    }

    @ViewBuilder private var statusActions: some View {
        if appState.rekordboxPlan != nil {
            Button("Dry run") { Task { await appState.applyRekordbox(dryRun: true) } }
                .constrained(by: gate.blocksDryRun ? gate.reason : appState.busyReason)
                .help("Runs the same apply path with dry_run: true; nothing is written.")
        }
        // C10 — the primary action names the blocker rather than being an
        // unexplained disabled button, so its own label is the reason. Only a
        // live run adds one the label cannot carry.
        Button(gate.actionLabel) { perform(gate.action) }
            .buttonStyle(.borderedProminent)
            .constrained(by: appState.busyReason)
            .help(gate.reason ?? "Applies the plan verbatim after a backup.")
    }

    // MARK: Gate

    private var gate: RekordboxApplyGate {
        RekordboxApplyGate(
            plan: appState.rekordboxPlan,
            obstacle: appState.rekordboxObstacle,
            isPlanning: isRunning(.plan)
        )
    }

    private func perform(_ action: RekordboxApplyGate.Action) {
        switch action {
        case .generatePlan: generatePlan()
        case .confirmApply: confirmingApply = true
        case .showBlockedRows: rowFilter = .blocked
        }
    }

    // MARK: Data

    private func generatePlan() {
        Task {
            await appState.planRekordbox(jobID: target.jobID, mappingID: target.mappingID)
        }
    }

    private func rows(_ plan: RekordboxPlanPresentation) -> [RekordboxPlanRowView] {
        let query = chrome.searchText.trimmingCharacters(in: .whitespaces).lowercased()
        return plan.rows.filter { row in
            guard rowFilter.includes(row) else { return false }
            guard !query.isEmpty else { return true }
            return row.title.lowercased().contains(query)
                || row.artist.lowercased().contains(query)
                || row.path.lowercased().contains(query)
        }
    }

    private func count(of option: RekordboxRowFilter) -> Int {
        (appState.rekordboxPlan?.rows ?? []).filter(option.includes).count
    }

    private func isRunning(_ kind: RekordboxOperationKind) -> Bool {
        switch (appState.rekordboxOperation, kind) {
        case (.plan, .plan), (.inspect, .inspect), (.apply, .apply): true
        default: false
        }
    }

    enum RekordboxOperationKind { case plan, inspect, apply }
}

/// The `rekordbox.plan` target, as a sidebar identity. `job_id` and
/// `mapping_id` are mutually exclusive on the wire, so they are one choice
/// here rather than two pickers that can contradict each other.
enum RekordboxPlanTarget: Equatable {
    case configDefault
    case job(String)
    case mapping(String)

    var id: String {
        switch self {
        case .configDefault: "__default__"
        case .job(let value): "job:\(value)"
        case .mapping(let value): "mapping:\(value)"
        }
    }

    init?(id: String) {
        if id == "__default__" {
            self = .configDefault
        } else if let value = id.split(separator: ":", maxSplits: 1).last.map(String.init), id.hasPrefix("job:") {
            self = .job(value)
        } else if let value = id.split(separator: ":", maxSplits: 1).last.map(String.init), id.hasPrefix("mapping:") {
            self = .mapping(value)
        } else {
            return nil
        }
    }

    var jobID: String? {
        if case .job(let value) = self { return value }
        return nil
    }

    var mappingID: String? {
        if case .mapping(let value) = self { return value }
        return nil
    }
}

enum RekordboxRowFilter: String, CaseIterable, Identifiable {
    case all
    case changes
    case blocked

    var id: String { rawValue }
    var label: String { rawValue.capitalized }

    func includes(_ row: RekordboxPlanRowView) -> Bool {
        switch self {
        case .all: true
        case .changes: row.isChange
        case .blocked: row.isBlocked
        }
    }
}
