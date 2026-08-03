import SwiftUI

/// Sync is three surfaces over one run, not three screens: you configure a
/// run, udl plans one source at a time and blocks on each (`SyncPlanView`),
/// then the run executes (`SyncRunView`). The plan surface wins over the run
/// surface because while a `ui.selectRows` request is open the backend is doing
/// nothing at all — the plan *is* what is happening.
struct SyncView: View {
    @EnvironmentObject private var appState: AppState

    var body: some View {
        if appState.planPrompt != nil {
            SyncPlanView()
        } else if appState.syncRun.phase.isActive || appState.syncRun.exitCode != nil {
            SyncRunView()
        } else {
            SyncConfigureView()
        }
    }
}

/// The pre-run surface. C1: there is no plan preview, so the only thing this
/// screen can do is choose sources and options and start a run.
struct SyncConfigureView: View {
    @EnvironmentObject private var appState: AppState

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                WorkspaceHeader(
                    title: "Run Sync",
                    // C1 — the plan lives inside a run and nowhere else.
                    lede: "udl has no plan preview. Planning happens inside a run, one source at a time, and the backend waits for your selection at each source. A dry run resolves every track and reports the outcome without writing files or touching the state file."
                )

                if let notResumed = appState.notResumedMessage(for: .sync) {
                    // C15 — a restart never replays mutating requests.
                    Callout(title: notResumed, severity: .warn)
                }

                Card(title: "Sources", subtitle: "sources.capabilities", flush: true) {
                    if appState.syncSources.isEmpty {
                        EmptyStateView(
                            title: "No sources",
                            kind: .empty,
                            detail: "sources.capabilities returned no configured sync sources."
                        )
                        .frame(height: 140)
                    } else {
                        ForEach(appState.syncSources) { source in
                            sourceRow(source)
                        }
                    }
                }

                if let validation = appState.syncValidationMessage {
                    Callout(title: validation, severity: .warn)
                }
            }
            .padding(.horizontal, Metrics.contentPaddingHorizontal)
            .padding(.vertical, Metrics.contentPadding)
        }
        .workspaceToolbar(title: "Run Sync", subtitle: "sync.start · plan: true")
        .workspaceInspector { inspector }
        .udlStatusBar {
            statusSummary
        } actions: {
            Button("Check system") { appState.destination = .doctor }
            Button(startLabel) { Task { await appState.startSync() } }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
        }
        .task {
            // Startup preloads this list. Avoid an immediate duplicate RPC
            // while the navigation view is still reconciling its controls.
            if appState.syncSources.isEmpty {
                await appState.loadSyncSources()
            }
        }
    }

    /// C1 — the button says which kind of run it starts, and dry run is the
    /// default, so the plan is always reachable through a reversible action.
    private var startLabel: String {
        appState.syncDryRun ? "Start dry run & plan" : "Start sync & plan"
    }

    private var selectedCount: Int {
        appState.syncSources.filter { appState.syncSourceOptions[$0.id]?.selected == true }.count
    }

    private func sourceRow(_ source: SourceCapability) -> some View {
        let options = appState.syncSourceOptions[source.id]
        return HStack(spacing: 12) {
            Toggle(
                "",
                isOn: Binding(
                    get: { options?.selected ?? false },
                    set: { appState.setSourceSelected(source.id, $0) }
                )
            )
            .labelsHidden()
            SourceGlyph(sourceType: source.sourceType)
            VStack(alignment: .leading, spacing: 1) {
                Text(source.sourceID).font(Typography.control).fontWeight(.medium)
                Text("\(source.sourceType) · \(source.adapter)")
                    .font(Typography.monoSmall)
                    .foregroundStyle(Theme.textTertiary)
            }
            Spacer(minLength: 8)

            if !source.supportsPlan {
                // C5 — a source that cannot be planned still runs; it just
                // never asks. Saying so beats a mystery missing prompt.
                ConstraintNote(text: "\(source.adapter) has no plan step, so this source never asks.")
            }

            // C5 — unsupported controls stay visible, disabled, and explained.
            Picker(
                "Window",
                selection: Binding(
                    get: { options?.planWindow ?? source.defaultPlanWindow },
                    set: { appState.setSourcePlanWindow(source.id, $0) }
                )
            ) {
                ForEach(PlanWindow.allCases) { Text($0.label).tag($0) }
            }
            .frame(width: 150)
            .constrained(by: source.supportsPlanWindow ? nil : "\(source.adapter) has no first/latest window.")

            Picker(
                "Order",
                selection: Binding(
                    get: { options?.downloadOrder ?? source.defaultDownloadOrder },
                    set: { appState.setSourceDownloadOrder(source.id, $0) }
                )
            ) {
                ForEach(DownloadOrder.allCases) { Text($0.label).tag($0) }
            }
            .frame(width: 180)
            .constrained(by: source.supportsDownloadOrder ? nil : "\(source.adapter) fixes the download order.")
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }

    @ViewBuilder private var inspector: some View {
        InspectorSection(title: "Run") {
            Toggle("Dry run", isOn: $appState.syncDryRun)
            Toggle("Unlimited plan", isOn: $appState.syncUnlimited)
            Stepper(
                "Plan limit: \(appState.syncUnlimited ? "∞" : String(appState.syncPlanLimit))",
                value: $appState.syncPlanLimit,
                in: 1...10_000
            )
            .constrained(by: appState.syncUnlimited ? "Unlimited is on, so no limit is sent." : nil)
            if appState.syncUnlimited {
                // C6 — unlimited is encoded as plan limit 0 on the wire.
                ConstraintNote(text: "∞ is sent as plan_limit: 0.")
            }
            FieldRow(label: "Timeout") {
                TextField("", value: $appState.syncTimeoutSeconds, format: .number)
                    .textFieldStyle(.roundedBorder)
                    .frame(width: 76)
            }
            ConstraintNote(text: "Timeout is in seconds; 0 means no timeout.")
        }

        // C17 — every one of these is in SyncStartParams and used to be
        // hardcoded in AppState.startSync(), which meant the GUI decided
        // silently on the user's behalf.
        InspectorSection(title: "Advanced") {
            Picker("Plan window", selection: $appState.syncPlanWindow) {
                ForEach(PlanWindow.allCases) { Text($0.label).tag($0) }
            }
            ConstraintNote(text: "The run-wide default. A per-source window above overrides it.")

            Picker("On existing files", selection: $appState.syncAskOnExisting) {
                ForEach(AskOnExistingPolicy.allCases) { Text($0.label).tag($0) }
            }
            ConstraintNote(text: "“udl decides” sends ask_on_existing_set: false and leaves the choice to the backend.")

            Toggle("Scan for gaps", isOn: $appState.syncScanGaps)
            Toggle("Skip preflight", isOn: $appState.syncNoPreflight)

            Picker("Existing-track status", selection: $appState.syncTrackStatus) {
                ForEach(TrackStatusMode.allCases) { Text($0.label).tag($0) }
            }
            ConstraintNote(text: "Spotify + deemix only; other adapters ignore track_status.")

            Button("Reset advanced to udl defaults") { appState.resetSyncAdvanced() }
                .buttonStyle(.link)
        }

        InspectorSection(title: "Sources") {
            FieldRow("Configured", "\(appState.syncSources.count)")
            FieldRow("Selected", "\(selectedCount)")
            FieldRow("Plan-capable", "\(appState.syncSources.filter(\.supportsPlan).count)")
            // C2 — set expectations before the run rather than after it stalls.
            ConstraintNote(text: "udl plans one source at a time; each source asks for its selection in turn and the backend waits.")
        }
    }

    private var statusSummary: some View {
        SummaryLine {
            SummaryCount(value: selectedCount, noun: "selected", severity: selectedCount > 0 ? .ok : .warn)
            Text("·")
            SummaryCount(value: appState.syncSources.count, noun: "configured")
            Text("·")
            Text(appState.syncDryRun ? "Dry run — nothing is written" : "Live run — files are written")
                .foregroundStyle(appState.syncDryRun ? Theme.textSecondary : Theme.warn)
        }
    }
}
