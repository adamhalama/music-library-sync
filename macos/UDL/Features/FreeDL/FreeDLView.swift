import SwiftUI

/// C7/C8 — Free DL as four explicit steps over one table.
///
/// `freedl.plan.start`, `freedl.capture.start`, `freedl.promotionPlan.build`
/// and `freedl.promote.apply` are four separate backend runs with four run IDs.
/// The phase is view `@State`: nothing here advances it, so a finished step
/// never silently hands you to the next one, and every completed step stays
/// visible and re-enterable. The table is the constant; only its columns and
/// the primary action change.
struct FreeDLView: View {
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var chrome: ShellChrome

    @State private var phase: FreeDLPhase = .plan
    @State private var jobID = ""
    @State private var filter: FreeDLRowFilter = .all
    @State private var cursor: FreeDLPlanRow.ID?
    /// A locked row that does nothing when clicked is a defect; clicking the
    /// lock states the backend's reason here.
    @State private var lockedReason: String?
    @State private var confirmingPromote = false

    var body: some View {
        content
            .workspaceToolbar(
                title: "SoundCloud Free DL",
                subtitle: "step \(phase.number) of 4 · \(phase.method)",
                searchable: true,
                searchPrompt: "Filter by title or remote ID"
            ) {
                if appState.freeDLRunID != nil {
                    Button("Cancel", role: .destructive) {
                        Task { await appState.cancelFreeDLOperation() }
                    }
                }
            }
            .workspaceInspector { inspector }
            .udlStatusBar { statusSummary } actions: { statusActions }
            .sidebarContext(sidebarContext)
            .task {
                if appState.freeDLConfig == nil {
                    await appState.loadFreeDLConfig()
                }
                selectDefaultJob()
            }
            .onChange(of: appState.freeDLConfig?.config.jobs.map(\.id) ?? []) { _, _ in
                selectDefaultJob()
            }
            .onDisappear { chrome.searchText = "" }
            .confirmationDialog(
                "Promote \(selectedPromotionCount) \(selectedPromotionCount == 1 ? "track" : "tracks")?",
                isPresented: $confirmingPromote,
                titleVisibility: .visible
            ) {
                Button("Back up and promote", role: .destructive) {
                    Task { await appState.applyFreeDLPromotion() }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(promotionConfirmationMessage)
            }
    }

    // MARK: Content

    /// `BoundedContent` gives the stack the definite height a `Table` needs in
    /// order to scroll instead of expanding through the toolbar and status bar.
    private var content: some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 0) {
                if let notResumed = appState.notResumedMessage(for: .freeDL) {
                    Callout(title: notResumed, severity: .warn)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.top, 12)
                }

                // A step that would not start is this screen's outcome. The
                // inspector's "Last message" is not enough — it can be closed.
                if let status = appState.freeDLStatus, status.severity != .info {
                    Callout(title: status.message, severity: status.severity)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.top, 12)
                }

                FreeDLPhaseBar(
                    current: phase,
                    lifecycle: lifecycle(of:),
                    unavailableReason: unavailableReason(for:),
                    select: { phase = $0; lockedReason = nil }
                )

                phaseNote

                if let busy = appState.busyReason {
                    ConstraintNote(text: "\(busy) Row selection is locked until it ends.")
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.bottom, 6)
                }

                phaseBody

                if let lockedReason {
                    ConstraintNote(text: lockedReason, severity: .warn)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.vertical, 8)
                }
            }
        }
    }

    /// What this step writes, plus whatever the running step is reporting.
    private var phaseNote: some View {
        HStack(spacing: 10) {
            Text(phase.effect)
                .font(Typography.caption)
                .foregroundStyle(Theme.textSecondary)
            Spacer(minLength: 12)
            if let stage = appState.freeDLStage, appState.freeDLRunID != nil {
                ProgressView().controlSize(.mini).scaleEffect(0.7)
                Text(stage)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
                    .lineLimit(1)
            }
            if phase == .plan || phase == .capture {
                Picker("Rows", selection: $filter) {
                    ForEach(FreeDLRowFilter.allCases) { option in
                        Text("\(option.label) \(count(of: option))").tag(option)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 330)
            } else {
                // The plan filter has nothing to act on here, so it is absent
                // rather than present and inert.
                Text("Promotion rows come from the capture run, not the plan.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.sunken)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }

    @ViewBuilder private var phaseBody: some View {
        switch phase {
        case .plan:
            if planRows.isEmpty {
                empty(
                    "Capture plan",
                    running: appState.freeDLOperation == .planning,
                    action: "build a capture plan to see candidate tracks"
                )
            } else {
                planTable
            }
        case .capture:
            if appState.freeDLCaptureRunID == nil, appState.freeDLOperation != .capture {
                empty("Capture", running: false, action: "capture the selected rows")
            } else {
                captureTable
            }
        case .promotionPlan:
            if let promotion = appState.freeDLPromotionPlan {
                promotionTable(promotion)
            } else {
                empty(
                    "Promotion plan",
                    running: appState.freeDLOperation == .promotionBuild,
                    action: "build a promotion plan from the captured files"
                )
            }
        case .promote:
            if let promotion = appState.freeDLPromotionPlan {
                promoteTable(promotion)
            } else {
                empty("Promote", running: appState.freeDLOperation == .promotionApply, action: "build a promotion plan first")
            }
        }
    }

    /// C7 — an unrun step reads "Not run yet", not an empty table.
    private func empty(_ title: String, running: Bool, action: String) -> some View {
        EmptyStateView(title: title, kind: running ? .working : .notRun(action: action))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: Tables

    private var planTable: some View {
        Table(planRows, selection: $cursor) {
            TableColumn("") { row in toggle(row) }.width(30)
            TableColumn("#") { row in
                Text(row.index + 1, format: .number)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            .width(38)
            TableColumn("Track") { row in
                Text(row.title).font(Typography.control).lineLimit(1)
            }
            // C8 — a cell that has not resolved says "checking" rather than
            // rendering blank, which would read as "nothing found".
            TableColumn("Local") { row in
                PendingValue(isResolved: row.localResolved) {
                    Text(row.localLabel)
                        .font(Typography.mono)
                        .foregroundStyle(row.localSeverity == .idle ? Theme.textSecondary : row.localSeverity.tint)
                }
            }
            .width(min: 110, ideal: 140)
            TableColumn("Free DL") { row in
                PendingValue(isResolved: row.freeDLResolved) {
                    Text(row.freeDLLabel)
                        .font(Typography.mono)
                        .foregroundStyle(row.freeDLSeverity == .idle ? Theme.textSecondary : row.freeDLSeverity.tint)
                }
            }
            .width(min: 110, ideal: 150)
            TableColumn("Status") { row in
                StatusPill(title: row.statusLabel, severity: row.statusSeverity)
            }
            .width(min: 84, ideal: 96)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
        .onChange(of: cursor) { _, _ in lockedReason = nil }
    }

    private var captureTable: some View {
        let captured = capturedRowsByRemoteID
        return Table(planRows) {
            TableColumn("#") { row in
                Text(row.index + 1, format: .number)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            .width(38)
            TableColumn("Track") { row in
                Text(row.title).font(Typography.control).lineLimit(1)
            }
            TableColumn("Free DL") { row in
                Text(row.freeDLLabel)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textSecondary)
            }
            .width(min: 110, ideal: 150)
            TableColumn("Capture") { row in
                // A row udl was never asked to capture is not "pending": it was
                // excluded, and says so.
                if let track = captured[row.remoteID] {
                    StatusPill(title: track.statusLabel, severity: captureSeverity(track))
                } else if isSelected(row) {
                    PendingValue(isResolved: false) { EmptyView() }
                } else {
                    Text("not selected")
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textTertiary)
                }
            }
            .width(min: 110, ideal: 150)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
    }

    private func promotionTable(_ promotion: FreeDLPromotionPlan) -> some View {
        Table(promotionRows(promotion)) {
            TableColumn("") { row in promotionToggle(row) }.width(30)
            TableColumn("#") { row in
                Text(row.index + 1, format: .number)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            .width(38)
            TableColumn("Track") { row in
                Text(row.title).font(Typography.control).lineLimit(1)
            }
            TableColumn("Was") { row in
                Text(row.originalQuality.summary)
                    .font(Typography.mono)
                    .foregroundStyle(row.originalQuality.severity.tint)
            }
            .width(min: 92, ideal: 110)
            TableColumn("Now") { row in
                Text(row.sourceQuality.summary)
                    .font(Typography.mono)
                    .foregroundStyle(row.sourceQuality.severity.tint)
            }
            .width(min: 92, ideal: 110)
            TableColumn("Score") { row in
                Text(row.score, format: .number)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textSecondary)
            }
            .width(50)
            TableColumn("Action") { row in
                StatusPill(
                    title: row.actionLabel,
                    severity: row.isApplicable ? .ok : .idle,
                    showsDot: false
                )
            }
            .width(min: 96, ideal: 110)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
    }

    /// Step 4 shows exactly what would be written and where the original goes,
    /// because that is the only irreversible step in the workflow.
    private func promoteTable(_ promotion: FreeDLPromotionPlan) -> some View {
        Table(promotionRows(promotion).filter { $0.isApplicable && $0.selected }) {
            TableColumn("Track") { row in
                Text(row.title).font(Typography.control).lineLimit(1)
            }
            TableColumn("Replaces") { row in
                Text(row.libraryPath).font(Typography.monoSmall).lineLimit(1).truncationMode(.middle)
            }
            TableColumn("Written to") { row in
                Text(row.outputPath).font(Typography.monoSmall).lineLimit(1).truncationMode(.middle)
            }
            TableColumn("Original copied to") { row in
                Text(row.backupPath).font(Typography.monoSmall).lineLimit(1).truncationMode(.middle)
            }
            TableColumn("Action") { row in
                StatusPill(title: row.actionLabel, severity: .warn, showsDot: false)
            }
            .width(min: 96, ideal: 110)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
    }

    @ViewBuilder
    private func toggle(_ row: FreeDLPlanRow) -> some View {
        if row.selectable {
            Toggle("", isOn: Binding(
                get: { isSelected(row) },
                set: { appState.setFreeDLSelection(row.remoteID, $0) }
            ))
            .labelsHidden()
            .disabled(appState.freeDLRunID != nil)
            .help(appState.busyReason ?? "Include this track in the capture.")
        } else {
            Button {
                lockedReason = row.lockReason
            } label: {
                Image(systemName: "lock.fill").foregroundStyle(Theme.textTertiary)
            }
            .buttonStyle(.plain)
            .help(row.lockReason)
            .accessibilityLabel("Locked: \(row.lockReason)")
        }
    }

    @ViewBuilder
    private func promotionToggle(_ row: FreeDLPromotionRow) -> some View {
        if row.isApplicable {
            Toggle("", isOn: Binding(
                get: { appState.freeDLPromotionOverrides[row.id] ?? row.selected },
                set: { appState.setFreeDLPromotionSelection(row.id, $0) }
            ))
            .labelsHidden()
            .disabled(appState.freeDLRunID != nil)
            .help(appState.busyReason ?? "Include this track in the promotion.")
        } else {
            Button {
                lockedReason = row.lockReason
            } label: {
                Image(systemName: "lock.fill").foregroundStyle(Theme.textTertiary)
            }
            .buttonStyle(.plain)
            .help(row.lockReason)
            .accessibilityLabel("Locked: \(row.lockReason)")
        }
    }

    // MARK: Sidebar

    private var sidebarContext: SidebarContext? {
        guard let config = appState.freeDLConfig else { return nil }
        let items = config.config.jobs.map { job in
            SidebarContextItem(
                id: job.id,
                title: job.id,
                subtitle: "\(job.targetFormat) · limit \(job.planLimit)",
                sourceType: "soundcloud",
                // No lifecycle chip here: a job is not a step, and the phase
                // bar already owns every step's state. Repeating it on the job
                // row only crowds out the job's name.
                lifecycle: nil,
                unavailableReason: job.enabled
                    ? (appState.freeDLRunID == nil ? nil : "A Free DL run is in flight; wait for it or cancel it.")
                    : "This job is disabled in freedl.yaml."
            )
        }
        return SidebarContext(
            title: "Free DL jobs",
            items: items,
            selectedID: jobID,
            note: "Capture writes to a buffer. Promotion is the only step that touches your library.",
            select: { id in
                guard id != jobID else { return }
                jobID = id
                // Switching jobs cannot carry another job's plan with it.
                phase = .plan
                lockedReason = nil
            }
        )
    }

    // MARK: Inspector

    @ViewBuilder private var inspector: some View {
        InspectorSection(title: "Steps") {
            // C7 — four runs, each with its own state, named by its own RPC.
            ForEach(FreeDLPhase.allCases) { step in
                FieldRow(label: step.title) { LifecycleChip(lifecycle: lifecycle(of: step)) }
                Text(step.method)
                    .font(Typography.monoSmall)
                    .foregroundStyle(Theme.textTertiary)
            }
            ConstraintNote(
                text: "Four separate backend runs with four run IDs. Nothing advances on its own, and a finished step never starts the next one."
            )
            if let runID = appState.freeDLRunID {
                FieldRow(label: "Live run") { MonoValue(text: runID) }
            }
            if let captureRunID = appState.freeDLCaptureRunID {
                FieldRow(label: "Capture run") { MonoValue(text: captureRunID) }
            }
        }

        if let status = appState.freeDLStatus {
            InspectorSection(title: "Last message") {
                Text(status.message)
                    .font(Typography.caption)
                    .foregroundStyle(status.severity == .info ? Theme.textSecondary : status.severity.tint)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }

        if phase == .plan || phase == .capture {
            InspectorSection(title: "Columns") {
                // Failure mode 3: the mockup's Score and Artist columns have no
                // source at plan time, so they are absent, not invented.
                ConstraintNote(
                    text: "freedl.plan.event sends title, local quality, Free DL availability and a skip reason. There is no artist and no match score until a promotion plan is built, so neither column is shown here."
                )
            }
        }

        if let config = appState.freeDLConfig {
            FreeDLJobForm(config: config, jobID: jobID)
        } else {
            InspectorSection(title: "Job") {
                ConstraintNote(text: "freedl.config.read has not returned yet.")
            }
        }
    }

    // MARK: Status bar

    private var statusSummary: some View {
        SummaryLine {
            switch phase {
            case .plan:
                SummaryCount(value: selectedCount, noun: "selected", severity: selectedCount > 0 ? .ok : .idle)
                Text("·")
                SummaryCount(value: upgradeCount, noun: "upgrades", severity: .info)
                // C8 — a row udl has not finished checking is counted as
                // checking, never folded into "no gain".
                if checkingCount > 0 {
                    Text("·")
                    SummaryCount(value: checkingCount, noun: "still checking", severity: .info)
                }
                Text("·")
                SummaryCount(
                    value: appState.freeDLRows.count - upgradeCount - checkingCount,
                    noun: "no gain or no free DL",
                    severity: .idle
                )
            case .capture:
                let captured = capturedRowsByRemoteID.values
                SummaryCount(value: captured.filter { $0.runtimeStatus == "downloaded" }.count, noun: "captured", severity: .ok)
                Text("·")
                SummaryCount(value: captured.filter { $0.runtimeStatus == "failed" }.count, noun: "failed", severity: .error)
                Text("·")
                Text("into the buffer directory — the library is unchanged")
            case .promotionPlan:
                SummaryCount(value: selectedPromotionCount, noun: "selected", severity: selectedPromotionCount > 0 ? .ok : .idle)
                Text("·")
                SummaryCount(value: (appState.freeDLPromotionPlan?.rows.filter { !$0.isApplicable }.count) ?? 0, noun: "skipped by udl", severity: .idle)
                if let format = appState.freeDLPromotionPlan?.targetFormat {
                    Text("·")
                    Text("target \(format)").font(Typography.mono)
                }
            case .promote:
                if appState.freeDLPromotionApplied {
                    Text("Promotion applied").foregroundStyle(Theme.ok)
                    Text("·")
                    Text("originals are in the backup directory")
                } else {
                    SummaryCount(value: selectedPromotionCount, noun: "library files will be replaced", severity: .warn)
                    Text("·")
                    Text("originals are copied to the backup directory first")
                }
            }
        }
    }

    @ViewBuilder private var statusActions: some View {
        if phase != .plan {
            Button("Back to \(FreeDLPhase(rawValue: phase.rawValue - 1)?.title ?? "plan")") {
                phase = FreeDLPhase(rawValue: phase.rawValue - 1) ?? .plan
            }
        }
        Button(primaryLabel) { runCurrentStep() }
            .buttonStyle(.borderedProminent)
            .constrained(by: primaryDisabledReason)
            .help(phase.effect)
    }

    private var primaryLabel: String {
        switch phase {
        case .plan: jobID.isEmpty ? "Build capture plan" : "Build capture plan for \(jobID)"
        case .capture: "Capture \(selectedCount) \(selectedCount == 1 ? "track" : "tracks")"
        case .promotionPlan: appState.freeDLPromotionPlan == nil ? "Build promotion plan" : "Rebuild promotion plan"
        case .promote: "Promote \(selectedPromotionCount) \(selectedPromotionCount == 1 ? "track" : "tracks")…"
        }
    }

    /// Every disabled state names its reason, surfaced as the button's help and
    /// repeated by the phase bar's note where it blocks navigation too.
    private var primaryDisabledReason: String? {
        if appState.freeDLRunID != nil { return "A Free DL run is already in flight." }
        switch phase {
        case .plan:
            return jobID.isEmpty ? "No enabled Free DL job is selected." : nil
        case .capture:
            if appState.freeDLCapturePlan == nil { return "Build a capture plan first." }
            return selectedCount == 0 ? "Select at least one upgradable row on the Plan step." : nil
        case .promotionPlan:
            return appState.freeDLCaptureRunID == nil ? "Capture at least one track first." : nil
        case .promote:
            if appState.freeDLPromotionPlan == nil { return "Build a promotion plan first." }
            return selectedPromotionCount == 0 ? "Select at least one promotable row." : nil
        }
    }

    private func runCurrentStep() {
        switch phase {
        case .plan:
            Task { await appState.startFreeDLPlan(jobID: jobID) }
        case .capture:
            Task { await appState.startFreeDLCapture() }
        case .promotionPlan:
            Task { await appState.buildFreeDLPromotionPlan() }
        case .promote:
            confirmingPromote = true
        }
    }

    private var promotionConfirmationMessage: String {
        let backup = appState.freeDLPromotionPlan?.backupRoot ?? "the job's backup directory"
        return """
        Library files are replaced with the captured versions. Each original is copied to \
        \(backup) first, and a result log is written to the job's log directory.
        """
    }

    // MARK: Data

    private func lifecycle(of step: FreeDLPhase) -> Lifecycle {
        switch step {
        case .plan:
            if appState.freeDLOperation == .planning { return .planning }
            return appState.freeDLCapturePlan == nil ? .notRun : .done
        case .capture:
            if appState.freeDLOperation == .capture { return .running }
            return appState.freeDLCaptureRunID == nil ? .notRun : .done
        case .promotionPlan:
            if appState.freeDLOperation == .promotionBuild { return .running }
            return appState.freeDLPromotionPlan == nil ? .notRun : .done
        case .promote:
            if appState.freeDLOperation == .promotionApply { return .running }
            return appState.freeDLPromotionApplied ? .done : .notRun
        }
    }

    /// A step whose backend prerequisite does not exist yet is dimmed and
    /// states why, rather than being a control that silently does nothing.
    private func unavailableReason(for step: FreeDLPhase) -> String? {
        switch step {
        case .plan: nil
        case .capture: appState.freeDLCapturePlan == nil ? "freedl.plan.start has not produced a capture plan yet." : nil
        case .promotionPlan: appState.freeDLCaptureRunID == nil ? "A promotion plan is built from a finished capture run." : nil
        case .promote: appState.freeDLPromotionPlan == nil ? "There is no promotion plan to apply." : nil
        }
    }

    private var planRows: [FreeDLPlanRow] {
        let query = chrome.searchText.trimmingCharacters(in: .whitespaces).lowercased()
        return appState.freeDLRows
            .filter { filter.includes($0, overrides: appState.freeDLSelectionOverrides) }
            .filter { row in
                guard !query.isEmpty else { return true }
                return row.title.lowercased().contains(query) || row.remoteID.lowercased().contains(query)
            }
            .sorted { $0.index < $1.index }
    }

    private func promotionRows(_ promotion: FreeDLPromotionPlan) -> [FreeDLPromotionRow] {
        let query = chrome.searchText.trimmingCharacters(in: .whitespaces).lowercased()
        guard !query.isEmpty else { return promotion.rows }
        return promotion.rows.filter { $0.title.lowercased().contains(query) }
    }

    /// The capture run is a real sync over the same SoundCloud source, so its
    /// `TrackRow.remote_id` is the same identifier the plan rows carry.
    private var capturedRowsByRemoteID: [String: TrackRow] {
        var rows: [String: TrackRow] = [:]
        for snapshot in appState.freeDLCaptureSources.values {
            for row in snapshot.rows where !row.remoteID.isEmpty && row.runScope == "included" {
                rows[row.remoteID] = row
            }
        }
        return rows
    }

    private func captureSeverity(_ row: TrackRow) -> Severity {
        switch row.runtimeStatus {
        case "downloaded": .ok
        case "failed": .error
        case "skipped": .idle
        default: .info
        }
    }

    private func isSelected(_ row: FreeDLPlanRow) -> Bool {
        appState.freeDLSelectionOverrides[row.remoteID] ?? row.selected
    }

    private func count(of option: FreeDLRowFilter) -> Int {
        appState.freeDLRows.filter { option.includes($0, overrides: appState.freeDLSelectionOverrides) }.count
    }

    private var selectedCount: Int {
        appState.freeDLRows.filter { $0.selectable && isSelected($0) }.count
    }

    private var upgradeCount: Int {
        appState.freeDLRows.filter(\.selectable).count
    }

    private var checkingCount: Int {
        appState.freeDLRows.filter(\.isStillResolving).count
    }

    private var selectedPromotionCount: Int {
        (appState.freeDLPromotionPlan?.rows ?? []).filter {
            $0.isApplicable && (appState.freeDLPromotionOverrides[$0.id] ?? $0.selected)
        }.count
    }

    private func selectDefaultJob() {
        let jobs = appState.freeDLConfig?.config.jobs ?? []
        guard jobs.first(where: { $0.id == jobID }) == nil else { return }
        jobID = jobs.first(where: \.enabled)?.id ?? jobs.first?.id ?? ""
    }
}
