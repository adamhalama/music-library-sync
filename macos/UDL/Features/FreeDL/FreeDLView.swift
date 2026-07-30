import SwiftUI

struct FreeDLView: View {
    @EnvironmentObject private var appState: AppState
    @State private var jobID = ""
    @State private var filter = FreeDLRowFilter.all

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text("FREE DOWNLOAD PIPELINE").font(.caption.bold().monospaced()).foregroundStyle(.orange)
                    Text("Plan, capture, promote").font(.largeTitle.bold())
                }
                Spacer()
                if appState.freeDLRunID != nil {
                    Button("Cancel", role: .destructive) { Task { await appState.cancelFreeDLOperation() } }
                }
            }

            if let config = appState.freeDLConfig {
                Picker("Job", selection: $jobID) {
                    ForEach(config.config.jobs.filter(\.enabled)) { Text($0.id).tag($0.id) }
                }
                .frame(maxWidth: 360)
                .onAppear { jobID = jobID.isEmpty ? config.config.jobs.first(where: \.enabled)?.id ?? "" : jobID }
            }

            HStack {
                Button("Build capture plan") {
                    Task { await appState.startFreeDLPlan(jobID: jobID) }
                }
                .buttonStyle(.borderedProminent)
                .disabled(jobID.isEmpty || appState.freeDLRunID != nil)
                if let stage = appState.freeDLStage {
                    Text(stage).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer()
                Picker("Rows", selection: $filter) {
                    ForEach(FreeDLRowFilter.allCases) { Text($0.rawValue.capitalized).tag($0) }
                }
                .frame(width: 150)
            }

            Table(appState.freeDLRows.filter { filter.includes($0, overrides: appState.freeDLSelectionOverrides) }) {
                TableColumn("") { row in
                    Toggle("", isOn: Binding(
                        get: { appState.freeDLSelectionOverrides[row.remoteID] ?? row.selected },
                        set: { appState.setFreeDLSelection(row.remoteID, $0) }
                    ))
                    .labelsHidden()
                    .disabled(!row.selectable)
                }
                .width(28)
                TableColumn("Track", value: \.title)
                TableColumn("Local") { Text(rowLabel($0)) }.width(min: 120, ideal: 170)
                TableColumn("Free DL") { Text($0.freeDLProbe.status ?? "checking") }.width(120)
            }

            if !appState.freeDLCaptureSources.isEmpty {
                GroupBox("Capture progress") {
                    VStack(alignment: .leading, spacing: 8) {
                        ForEach(appState.freeDLCaptureSources.keys.sorted(), id: \.self) { sourceID in
                            if let source = appState.freeDLCaptureSources[sourceID] {
                                HStack {
                                    Text(sourceID).font(.headline)
                                    Spacer()
                                    Text("\(source.downloadedCount) done · \(source.skippedCount) skipped · \(source.failedCount) failed")
                                        .font(.caption.monospaced())
                                    Text(source.lifecycle.uppercased()).font(.caption.bold().monospaced())
                                }
                                ForEach(source.rows.filter { $0.runScope == "included" }) { row in
                                    HStack {
                                        Text(row.title).lineLimit(1)
                                        Spacer()
                                        Text(row.statusLabel).font(.caption.monospaced()).foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                }
            }

            HStack {
                Text("\(selectedCount) selected")
                    .font(.caption.bold().monospaced())
                Spacer()
                if appState.freeDLCapturePlan != nil {
                    Button("Capture selected") { Task { await appState.startFreeDLCapture() } }
                        .disabled(selectedCount == 0 || appState.freeDLRunID != nil)
                    Button("Build promotion plan") { Task { await appState.buildFreeDLPromotionPlan() } }
                        .disabled(appState.freeDLCaptureRunID == nil || appState.freeDLRunID != nil)
                }
            }

            if let promotion = appState.freeDLPromotionPlan {
                Divider()
                Text("Promotion review").font(.title2.bold())
                Table(promotion.rows) {
                    TableColumn("Track", value: \.title)
                    TableColumn("Action", value: \.action).width(110)
                    TableColumn("Score") { Text(String($0.score)) }.width(55)
                    TableColumn("Destination") { Text($0.outputPath).lineLimit(1) }
                    TableColumn("Reason") { Text($0.reason ?? "Ready") }
                }
                .frame(minHeight: 180)
                HStack {
                    Text("Files are applied only from the backend-rebuilt authoritative plan.")
                        .font(.caption).foregroundStyle(.secondary)
                    Spacer()
                    Button("Apply promotion") { Task { await appState.applyFreeDLPromotion() } }
                        .buttonStyle(.borderedProminent)
                        .disabled(appState.freeDLRunID != nil)
                }
            }

            if let status = appState.freeDLStatusMessage {
                Text(status).foregroundStyle(.secondary)
            }
        }
        .padding(28)
        .task {
            if appState.freeDLConfig == nil {
                await appState.loadFreeDLConfig()
            }
        }
    }

    private var selectedCount: Int {
        appState.freeDLRows.filter {
            $0.selectable && (appState.freeDLSelectionOverrides[$0.remoteID] ?? $0.selected)
        }.count
    }

    private func rowLabel(_ row: FreeDLPlanRow) -> String {
        if let reason = row.skipReason, !reason.isEmpty { return reason }
        if let state = row.localState { return state.replacingOccurrences(of: "_", with: " ") }
        return "checking"
    }
}
