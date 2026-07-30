import SwiftUI

struct SyncView: View {
    @EnvironmentObject private var appState: AppState
    @State private var rowFilter: SyncRowFilter = .all

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                header
                if appState.syncRun.phase.isActive || appState.syncRun.exitCode != nil {
                    runDetail
                } else {
                    configuration
                }
            }
            .padding(30)
            .frame(maxWidth: 1100, alignment: .leading)
        }
        .task {
            // Startup preloads this list. Avoid an immediate duplicate RPC
            // while the navigation view is still reconciling its controls.
            if appState.syncSources.isEmpty {
                await appState.loadSyncSources()
            }
        }
    }

    private var header: some View {
        HStack {
            VStack(alignment: .leading, spacing: 5) {
                Text("INTERACTIVE SYNC")
                    .font(.caption.bold().monospaced())
                    .foregroundStyle(.orange)
                Text("Build the run. Keep the signal.")
                    .font(.largeTitle.bold())
            }
            Spacer()
            if appState.syncRun.phase.isActive {
                Button("Cancel run", role: .destructive) {
                    Task { await appState.cancelActiveSync() }
                }
            }
        }
    }

    private var configuration: some View {
        VStack(alignment: .leading, spacing: 18) {
            GroupBox("Sources") {
                VStack(spacing: 0) {
                    ForEach(appState.syncSources) { source in
                        sourceRow(source)
                        if source.id != appState.syncSources.last?.id { Divider() }
                    }
                }
                .padding(.vertical, 4)
            }

            GroupBox("Run controls") {
                Grid(alignment: .leading, horizontalSpacing: 22, verticalSpacing: 14) {
                    GridRow {
                        Toggle("Dry run", isOn: $appState.syncDryRun)
                        Toggle("Unlimited", isOn: $appState.syncUnlimited)
                    }
                    GridRow {
                        Stepper(
                            "Plan limit: \(appState.syncUnlimited ? "unlimited" : String(appState.syncPlanLimit))",
                            value: $appState.syncPlanLimit,
                            in: 1...10_000
                        )
                        TextField("Timeout seconds", value: $appState.syncTimeoutSeconds, format: .number)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 210)
                    }
                }
                .padding(8)
            }

            HStack {
                if let validation = appState.syncValidationMessage {
                    Label(validation, systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                }
                Spacer()
                Button("Start sync") {
                    Task { await appState.startSync() }
                }
                .buttonStyle(.borderedProminent)
                .disabled(appState.syncRun.phase.isActive)
            }
        }
    }

    private func sourceRow(_ source: SourceCapability) -> some View {
        let options = appState.syncSourceOptions[source.id]
        return HStack(spacing: 14) {
            Toggle(
                "",
                isOn: Binding(
                    get: { options?.selected ?? false },
                    set: { appState.setSourceSelected(source.id, $0) }
                )
            )
            .labelsHidden()
            VStack(alignment: .leading, spacing: 2) {
                Text(source.sourceID).font(.headline)
                Text("\(source.sourceType) · \(source.adapter)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if source.supportsPlanWindow {
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
            }
            if source.supportsDownloadOrder {
                Picker(
                    "Order",
                    selection: Binding(
                        get: { options?.downloadOrder ?? source.defaultDownloadOrder },
                        set: { appState.setSourceDownloadOrder(source.id, $0) }
                    )
                ) {
                    ForEach(DownloadOrder.allCases) { Text($0.label).tag($0) }
                }
                .frame(width: 190)
            }
        }
        .padding(.vertical, 10)
    }

    private var runDetail: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Label(appState.syncRun.phase.rawValue.replacingOccurrences(of: "_", with: " ").capitalized,
                      systemImage: appState.syncRun.phase.isActive ? "waveform" : "checkmark.circle")
                    .font(.title2.bold())
                Spacer()
                Picker("Rows", selection: $rowFilter) {
                    ForEach(SyncRowFilter.allCases) { Text($0.rawValue.capitalized).tag($0) }
                }
                .frame(width: 170)
            }

            if let progress = appState.syncRun.progress {
                progressHeader(progress)
            }

            ForEach(appState.syncRun.sources.keys.sorted(), id: \.self) { sourceID in
                if let source = appState.syncRun.sources[sourceID] {
                    GroupBox {
                        VStack(alignment: .leading, spacing: 10) {
                            HStack {
                                Text(sourceID).font(.headline)
                                Spacer()
                                Text("\(source.downloadedCount) done · \(source.skippedCount) skipped · \(source.failedCount) failed · \(source.includedCount) selected")
                                    .font(.caption.monospaced())
                                    .foregroundStyle(.secondary)
                                Text(source.lifecycle.uppercased())
                                    .font(.caption.bold().monospaced())
                                    .foregroundStyle(.secondary)
                            }
                            ForEach(source.rows.filter(rowFilter.includes)) { row in
                                HStack {
                                    Image(systemName: icon(for: row.runtimeStatus))
                                        .foregroundStyle(color(for: row.runtimeStatus))
                                        .frame(width: 18)
                                    Text(row.title).lineLimit(1)
                                    Spacer()
                                    Text(row.statusLabel)
                                        .font(.caption.monospaced())
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                }
            }

            GroupBox("Activity") {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(Array(appState.syncRun.activity.suffix(80).enumerated()), id: \.offset) { _, event in
                        HStack(alignment: .firstTextBaseline) {
                            Text(event.level.uppercased())
                                .font(.caption2.bold().monospaced())
                                .foregroundStyle(event.level == "error" ? .red : .secondary)
                                .frame(width: 55, alignment: .leading)
                            Text(event.message).font(.callout.monospaced())
                        }
                    }
                    if appState.syncRun.activity.isEmpty {
                        Text("Waiting for backend events…").foregroundStyle(.secondary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }

            if let message = appState.syncRun.terminalMessage {
                Text(message).foregroundStyle(appState.syncRun.phase == .succeeded ? .green : .orange)
            }
            if !appState.syncRun.phase.isActive {
                Button("Configure another run") { appState.resetSyncRun() }
            }
        }
    }

    private func progressHeader(_ snapshot: StructuredProgressSnapshot) -> some View {
        let global = snapshot.progress.global
        let total = max(global.total, 0)
        let completed = min(max(global.completed, 0), total)
        let track = snapshot.track
        return GroupBox("Progress") {
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    Text(total > 0 ? "\(completed) of \(total) tracks" : "Preparing run…")
                        .font(.caption.bold().monospaced())
                    Spacer()
                    if !snapshot.progress.source.id.isEmpty {
                        Text(snapshot.progress.source.id)
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                    }
                }
                ProgressView(
                    value: total > 0 ? Double(completed) : 0,
                    total: Double(max(total, 1))
                )

                if !track.name.isEmpty {
                    HStack {
                        Text(track.name).lineLimit(1)
                        Spacer()
                        Text(track.lifecycle.uppercased())
                            .font(.caption2.bold().monospaced())
                            .foregroundStyle(.secondary)
                    }
                    if track.progressKnown {
                        ProgressView(value: min(max(track.progressPercent, 0), 100), total: 100)
                            .tint(.orange)
                    }
                }
            }
            .padding(4)
        }
    }

    private func icon(for status: String) -> String {
        switch status {
        case "downloaded": "checkmark.circle.fill"
        case "failed": "xmark.octagon.fill"
        case "skipped": "forward.circle.fill"
        case "downloading": "arrow.down.circle.fill"
        default: "circle"
        }
    }

    private func color(for status: String) -> Color {
        switch status {
        case "downloaded": .green
        case "failed": .red
        case "skipped": .orange
        case "downloading": .blue
        default: .secondary
        }
    }
}
