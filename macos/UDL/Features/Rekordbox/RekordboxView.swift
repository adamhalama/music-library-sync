import SwiftUI

struct RekordboxView: View {
    @EnvironmentObject private var appState: AppState
    @State private var jobID = ""
    @State private var mappingID = ""
    @State private var showingConfig = false
    @State private var confirmingApply = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                header
                runtimeCard
                configurationCard
                inspectionCard
                planCard
                if let message = appState.rekordboxStatusMessage {
                    Text(message).foregroundStyle(.secondary).textSelection(.enabled)
                }
            }
            .padding(28)
            .frame(maxWidth: 1100, alignment: .leading)
        }
        .task {
            if appState.rekordboxConfig == nil || appState.rekordboxRuntime == nil {
                await appState.loadRekordbox()
            }
        }
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
            Button("Apply to isolated/selected database", role: .destructive) {
                Task { await appState.applyRekordbox(dryRun: false) }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The backend will re-verify integrity, require Rekordbox to be closed, create a backup, then verify the result.")
        }
    }

    private var header: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("REKORDBOX MIRROR").font(.caption.bold().monospaced()).foregroundStyle(.orange)
                Text("Inspect, plan, verify, apply").font(.largeTitle.bold())
            }
            Spacer()
            if appState.rekordboxRunID != nil {
                Button("Cancel operation", role: .destructive) {
                    Task { await appState.cancelRekordboxOperation() }
                }
            }
        }
    }

    private var runtimeCard: some View {
        GroupBox("Managed Python runtime") {
            HStack {
                if let runtime = appState.rekordboxRuntime?.status {
                    Image(systemName: runtime.healthy ? "checkmark.seal.fill" : "wrench.and.screwdriver.fill")
                        .foregroundStyle(runtime.healthy ? .green : .orange)
                    VStack(alignment: .leading) {
                        Text(runtime.message)
                        Text("\(runtime.pythonBin) · pyrekordbox \(runtime.version ?? "not verified")")
                            .font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
                    }
                } else {
                    ProgressView()
                }
                Spacer()
                Button("Ensure") { Task { await appState.ensureRekordboxRuntime() } }
                Button("Reset…", role: .destructive) { Task { await appState.resetRekordboxRuntime() } }
            }
            .padding(6)
        }
    }

    private var configurationCard: some View {
        GroupBox("Configuration") {
            HStack {
                if let config = appState.rekordboxConfig {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(config.path.path).font(.caption.monospaced()).textSelection(.enabled)
                        Text("Database: \(config.config.defaults.dbDir)")
                        Text("Backups: \(config.config.defaults.backupDir)")
                    }
                }
                Spacer()
                Button("Edit paths & mappings…") { showingConfig = true }
            }
            .padding(6)
        }
    }

    private var inspectionCard: some View {
        GroupBox("Read-only inspection") {
            HStack {
                if let inspect = appState.rekordboxInspect {
                    Text("\(inspect.playlists.count) playlists · \(inspect.contents.count) tracks")
                } else {
                    Text("Inspect the selected database before planning.")
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Button("Inspect database") { Task { await appState.inspectRekordbox() } }
            }
            .padding(6)
        }
    }

    private var planCard: some View {
        GroupBox("Checksummed mirror plan") {
            VStack(alignment: .leading, spacing: 14) {
                if let config = appState.rekordboxConfig?.config {
                    HStack {
                        Picker("Playlist job", selection: $jobID) {
                            Text("Default").tag("")
                            ForEach(config.sync.jobs) { Text($0.id).tag($0.id) }
                        }
                        Picker("Folder mapping", selection: $mappingID) {
                            Text("None").tag("")
                            ForEach(config.sync.folders) { Text($0.id).tag($0.id) }
                        }
                        Button("Generate plan") {
                            Task {
                                await appState.planRekordbox(
                                    jobID: jobID.isEmpty ? nil : jobID,
                                    mappingID: mappingID.isEmpty ? nil : mappingID
                                )
                            }
                        }
                        .buttonStyle(.borderedProminent)
                    }
                }

                if let plan = appState.rekordboxPlan {
                    HStack(spacing: 20) {
                        metric("Total", plan.musicTotal)
                        metric("Matched", plan.matched)
                        metric("Missing", plan.missing)
                        metric("Ambiguous", plan.ambiguous)
                        Spacer()
                        Text("v\(plan.version) · \(String(plan.checksum.prefix(12)))")
                            .font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                    Table(plan.rows) {
                        TableColumn("Artist", value: \.artist)
                        TableColumn("Title", value: \.title)
                        TableColumn("Match", value: \.matchStatus).width(100)
                        TableColumn("Action", value: \.action).width(100)
                        TableColumn("Expected path") { Text($0.path).lineLimit(1) }
                    }
                    .frame(minHeight: 250)
                    if !plan.blockers.isEmpty {
                        Label(
                            "\(plan.blockers.count) blockers prevent apply. The complete mirror remains visible; no partial request will be sent.",
                            systemImage: "hand.raised.fill"
                        )
                        .foregroundStyle(.red)
                    }
                    HStack {
                        Button("Dry run") { Task { await appState.applyRekordbox(dryRun: true) } }
                            .disabled(!plan.blockers.isEmpty)
                        Spacer()
                        Button("Apply…") { confirmingApply = true }
                            .buttonStyle(.borderedProminent)
                            .disabled(!plan.blockers.isEmpty || plan.checksum.isEmpty)
                    }
                }
            }
            .padding(6)
        }
    }

    private func metric(_ label: String, _ value: Int) -> some View {
        VStack(alignment: .leading) {
            Text(String(value)).font(.title2.bold().monospaced())
            Text(label).font(.caption).foregroundStyle(.secondary)
        }
    }
}

private struct RekordboxConfigEditor: View {
    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss
    @State var config: RekordboxConfig
    @State private var mappingID = ""
    @State private var musicFolder = ""
    @State private var rekordboxFolder = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Rekordbox configuration").font(.title2.bold())
            Form {
                TextField("Database directory", text: $config.defaults.dbDir)
                TextField("Backup directory", text: $config.defaults.backupDir)
                TextField(
                    "Python binary",
                    text: Binding(get: { config.defaults.pythonBin ?? "" }, set: { config.defaults.pythonBin = $0.isEmpty ? nil : $0 })
                )
                Section("Folder mappings") {
                    ForEach(config.sync.folders) { mapping in
                        LabeledContent(mapping.id, value: "\(mapping.musicFolder ?? mapping.musicFolderID ?? "—") → \(mapping.rekordboxFolder ?? mapping.rekordboxFolderID ?? "—")")
                    }
                    TextField("Mapping ID", text: $mappingID)
                    TextField("Music folder", text: $musicFolder)
                    TextField("Rekordbox folder", text: $rekordboxFolder)
                    Button("Add mapping") {
                        config.sync.folders.append(RekordboxFolderMapping(
                            id: mappingID, musicFolder: musicFolder, musicFolderID: nil,
                            rekordboxFolder: rekordboxFolder, rekordboxFolderID: nil,
                            playlistNameMap: nil, includePlaylists: nil, excludePlaylists: nil,
                            onMissingTracks: "fail", createFolders: nil, createPlaylists: nil
                        ))
                        mappingID = ""; musicFolder = ""; rekordboxFolder = ""
                    }
                    .disabled(mappingID.isEmpty || musicFolder.isEmpty || rekordboxFolder.isEmpty)
                }
            }
            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                Button("Save canonical config") {
                    Task {
                        if await appState.saveRekordboxConfig(config) { dismiss() }
                    }
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .padding(24)
        .frame(width: 720, height: 620)
    }
}
