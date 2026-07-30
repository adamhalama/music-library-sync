import SwiftUI

struct ConfigEditorView: View {
    @EnvironmentObject private var appState: AppState
    @State private var draft: MainConfig?
    @State private var showingSourceEditor = false
    @State private var editingIndex: Int?
    @State private var deletingIndex: Int?

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                VStack(alignment: .leading) {
                    Text("CANONICAL CONFIGURATION").font(.caption.bold().monospaced()).foregroundStyle(.orange)
                    Text("Defaults and sources").font(.largeTitle.bold())
                }
                Spacer()
                Button("Reload") { Task { await appState.loadConfigEditor(); loadDraft() } }
                Button("Save") {
                    guard let draft else { return }
                    Task { _ = await appState.saveMainConfig(draft) }
                }
                .buttonStyle(.borderedProminent)
                .disabled(draft == nil)
            }

            if let file = appState.configFile {
                Text(file.path).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }

            if !appState.configProblems.isEmpty {
                GroupBox("Validation problems") {
                    VStack(alignment: .leading) {
                        ForEach(appState.configProblems, id: \.self) {
                            Label($0, systemImage: "exclamationmark.triangle").foregroundStyle(.red)
                        }
                    }
                }
            }

            if let draftBinding = Binding($draft) {
                Form {
                    Section("Defaults") {
                        TextField("State directory", text: draftBinding.defaults.stateDir)
                        TextField("Archive file", text: draftBinding.defaults.archiveFile)
                        Stepper("Threads: \(draftBinding.wrappedValue.defaults.threads)",
                                value: draftBinding.defaults.threads, in: 1...128)
                        Stepper("Command timeout: \(draftBinding.wrappedValue.defaults.commandTimeoutSeconds)s",
                                value: draftBinding.defaults.commandTimeoutSeconds, in: 1...86_400)
                        Toggle("Continue on error", isOn: draftBinding.defaults.continueOnError)
                    }
                    Section("Sources") {
                        ForEach(Array(draftBinding.wrappedValue.sources.enumerated()), id: \.element.id) { index, source in
                            HStack {
                                VStack(alignment: .leading) {
                                    Text(source.id)
                                        .foregroundStyle(hasProblem(for: source) ? Color.red : Color.primary)
                                    Text("\(source.type) · \(source.adapter.kind) · \(source.targetDir)")
                                        .font(.caption.monospaced()).foregroundStyle(.secondary)
                                }
                                Spacer()
                                Toggle(
                                    "Enabled",
                                    isOn: Binding(
                                        get: { draft?.sources[index].enabled ?? false },
                                        set: { draft?.sources[index].enabled = $0 }
                                    )
                                )
                                Button("Edit") { editingIndex = index; showingSourceEditor = true }
                                Button(role: .destructive) { deletingIndex = index } label: {
                                    Image(systemName: "trash")
                                }
                            }
                        }
                        .onMove { offsets, destination in
                            draft?.sources.move(fromOffsets: offsets, toOffset: destination)
                        }
                        Button("Add source") { editingIndex = nil; showingSourceEditor = true }
                    }
                    Section("Feature configuration paths") {
                        ForEach((appState.initialization?.featureConfigPaths ?? [:]).keys.sorted(), id: \.self) { feature in
                            LabeledContent(feature) {
                                Text((appState.initialization?.featureConfigPaths[feature] ?? []).joined(separator: "\n"))
                                    .font(.caption.monospaced()).textSelection(.enabled)
                            }
                        }
                    }
                }
            } else {
                ProgressView()
            }

            if let message = appState.configStatusMessage {
                Text(message).foregroundStyle(.secondary)
            }
        }
        .padding(28)
        .task {
            if appState.configFile == nil { await appState.loadConfigEditor() }
            loadDraft()
        }
        .sheet(isPresented: $showingSourceEditor) {
            SourceEditorView(source: editingIndex.flatMap { draft?.sources[$0] }) { source in
                if let index = editingIndex {
                    draft?.sources[index] = source
                } else {
                    draft?.sources.append(source)
                }
            }
        }
        .confirmationDialog(
            "Delete this source?",
            isPresented: Binding(
                get: { deletingIndex != nil },
                set: { if !$0 { deletingIndex = nil } }
            )
        ) {
            Button("Delete source", role: .destructive) {
                if let index = deletingIndex { draft?.sources.remove(at: index) }
                deletingIndex = nil
            }
            Button("Cancel", role: .cancel) { deletingIndex = nil }
        } message: {
            Text("Deletion is only written when you explicitly save.")
        }
    }

    private func loadDraft() {
        draft = appState.configFile?.config
    }

    private func hasProblem(for source: MainConfigSource) -> Bool {
        appState.configProblems.contains {
            $0.localizedCaseInsensitiveContains(source.id) ||
                $0.localizedCaseInsensitiveContains("sources")
        }
    }
}

struct SourceEditorView: View {
    @Environment(\.dismiss) private var dismiss
    let onSave: (MainConfigSource) -> Void
    @State private var id: String
    @State private var type: String
    @State private var enabled: Bool
    @State private var targetDir: String
    @State private var url: String
    @State private var stateFile: String
    @State private var adapter: String
    @State private var extraArgs: String
    @State private var breakOnExisting: Bool
    @State private var askOnExisting: Bool
    @State private var localIndexCache: Bool

    init(source: MainConfigSource? = nil, onSave: @escaping (MainConfigSource) -> Void) {
        self.onSave = onSave
        _id = State(initialValue: source?.id ?? "")
        _type = State(initialValue: source?.type ?? "soundcloud")
        _enabled = State(initialValue: source?.enabled ?? true)
        _targetDir = State(initialValue: source?.targetDir ?? "")
        _url = State(initialValue: source?.url ?? "")
        _stateFile = State(initialValue: source?.stateFile ?? "")
        _adapter = State(initialValue: source?.adapter.kind ?? "scdl")
        _extraArgs = State(initialValue: source?.adapter.extraArgs?.joined(separator: "\n") ?? "")
        _breakOnExisting = State(initialValue: source?.sync.breakOnExisting ?? true)
        _askOnExisting = State(initialValue: source?.sync.askOnExisting ?? false)
        _localIndexCache = State(initialValue: source?.sync.localIndexCache ?? true)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Source").font(.title2.bold())
            Form {
                TextField("ID", text: $id)
                Picker("Type", selection: $type) {
                    Text("SoundCloud").tag("soundcloud")
                    Text("Spotify").tag("spotify")
                }
                Toggle("Enabled", isOn: $enabled)
                TextField("Source URL", text: $url)
                TextField("Target directory", text: $targetDir)
                TextField("State file", text: $stateFile)
                TextField("Adapter", text: $adapter)
                TextField("Adapter arguments, one per line", text: $extraArgs, axis: .vertical)
                    .lineLimit(3...8)
                Toggle("Break on existing", isOn: $breakOnExisting)
                Toggle("Ask on existing", isOn: $askOnExisting)
                Toggle("Use local index cache", isOn: $localIndexCache)
            }
            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                Button("Use source") {
                    onSave(MainConfigSource(
                        id: id.trimmingCharacters(in: .whitespacesAndNewlines),
                        type: type, enabled: enabled, targetDir: targetDir, url: url,
                        stateFile: stateFile.isEmpty ? nil : stateFile,
                        sync: SourceSyncPolicy(
                            breakOnExisting: breakOnExisting,
                            askOnExisting: askOnExisting,
                            localIndexCache: localIndexCache
                        ),
                        adapter: SourceAdapter(
                            kind: adapter,
                            extraArgs: extraArgs.split(separator: "\n").map(String.init),
                            minVersion: nil
                        )
                    ))
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
                .disabled(id.isEmpty || url.isEmpty || targetDir.isEmpty || adapter.isEmpty)
            }
        }
        .padding(24)
        .frame(width: 680, height: 660)
    }
}
