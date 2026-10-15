import SwiftUI

/// Creating a source, which is the one thing the inline form cannot do: an id
/// is fixed once it exists, so it is chosen here.
///
/// Also used by Onboarding to draft the very first source before any config
/// file exists.
struct SourceEditorView: View {
    @Environment(\.dismiss) private var dismiss
    let existingIDs: [String]
    let onSave: (MainConfigSource) -> Void

    @State private var id: String
    @State private var type: String
    @State private var enabled: Bool
    @State private var targetDir: String
    @State private var url: String
    @State private var stateFile: String
    @State private var adapter: String
    @State private var extraArgs: String
    @State private var breakOnExisting: TriStateBool
    @State private var askOnExisting: TriStateBool
    @State private var localIndexCache: TriStateBool

    init(
        source: MainConfigSource? = nil,
        existingIDs: [String] = [],
        onSave: @escaping (MainConfigSource) -> Void
    ) {
        self.existingIDs = existingIDs
        self.onSave = onSave
        _id = State(initialValue: source?.id ?? "")
        _type = State(initialValue: source?.type ?? "soundcloud")
        _enabled = State(initialValue: source?.enabled ?? true)
        _targetDir = State(initialValue: source?.targetDir ?? "")
        _url = State(initialValue: source?.url ?? "")
        _stateFile = State(initialValue: source?.stateFile ?? "")
        _adapter = State(initialValue: source?.adapter.kind ?? "scdl")
        _extraArgs = State(initialValue: source?.adapter.extraArgs?.joined(separator: "\n") ?? "")
        _breakOnExisting = State(initialValue: TriStateBool(source?.sync.breakOnExisting))
        _askOnExisting = State(initialValue: TriStateBool(source?.sync.askOnExisting))
        _localIndexCache = State(initialValue: TriStateBool(source?.sync.localIndexCache))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Source").font(Typography.sectionTitle)
            Form {
                Section("Identity") {
                    TextField("ID", text: $id)
                    if isDuplicateID {
                        ConstraintNote(
                            text: "A source with this id already exists. Ids name state files, so udl rejects two of them.",
                            severity: .error,
                            symbol: "exclamationmark.octagon"
                        )
                    }
                    Picker("Type", selection: $type) {
                        Text("SoundCloud").tag("soundcloud")
                        Text("Spotify").tag("spotify")
                    }
                    Toggle("Enabled", isOn: $enabled)
                }
                Section("Location") {
                    TextField("Source URL", text: $url)
                    HStack(spacing: 8) {
                        TextField("Target directory", text: $targetDir)
                        Button("Choose…") { chooseDirectory(into: $targetDir) }
                    }
                    TextField("State file", text: $stateFile)
                }
                Section("Sync policy") {
                    triState("Break on existing", selection: $breakOnExisting)
                    triState("Ask on existing", selection: $askOnExisting)
                    triState("Local index cache", selection: $localIndexCache)
                    ConstraintNote(
                        text: "Default leaves the key out of the file entirely, which is not the same as writing false."
                    )
                }
                Section("Adapter") {
                    TextField("Kind", text: $adapter)
                    TextField("Arguments, one per line", text: $extraArgs, axis: .vertical)
                        .lineLimit(3...8)
                }
            }
            .formStyle(.grouped)

            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                // Failure mode 1 — the reason a disabled primary is disabled
                // has to be on screen, not only in a tooltip.
                if let missing = missingRequirement {
                    ConstraintNote(text: missing)
                }
                Button("Use source") { save() }
                    .buttonStyle(.borderedProminent)
                    .disabled(!isValid)
                    .help(isValid
                          ? "Adds the source to the draft. Nothing is written until you save."
                          : "A unique id, a url, a target directory, a state file and an adapter kind are all required.")
            }
        }
        .padding(20)
        .frame(width: 680, height: 660)
    }

    private func triState(_ title: String, selection: Binding<TriStateBool>) -> some View {
        Picker(title, selection: selection) {
            ForEach(TriStateBool.allCases) { state in
                Text(state.label).tag(state)
            }
        }
    }

    private var isDuplicateID: Bool { existingIDs.contains(id.trimmed) }

    private var isValid: Bool {
        !id.trimmed.isEmpty && !url.trimmed.isEmpty && !targetDir.trimmed.isEmpty
            && !adapter.trimmed.isEmpty && !stateFile.trimmed.isEmpty && !isDuplicateID
    }

    /// Names the first thing still missing, so the disabled primary is never a
    /// dead control. `isDuplicateID` already has its own note next to the field.
    private var missingRequirement: String? {
        if isValid || isDuplicateID { return nil }
        if id.trimmed.isEmpty { return "An id is required; it names this source's state file." }
        if url.trimmed.isEmpty { return "A source URL is required." }
        if targetDir.trimmed.isEmpty { return "A target directory is required." }
        if adapter.trimmed.isEmpty { return "An adapter kind is required." }
        // config.validate rejects both source types without one, so the editor
        // asks for it rather than letting the save fail after the fact.
        if stateFile.trimmed.isEmpty { return "A state file is required for both soundcloud and spotify sources." }
        return nil
    }

    private func save() {
        let args = extraArgs
            .split(separator: "\n", omittingEmptySubsequences: true)
            .map { String($0).trimmed }
            .filter { !$0.isEmpty }
        onSave(MainConfigSource(
            id: id.trimmed,
            type: type,
            enabled: enabled,
            targetDir: targetDir.trimmed,
            url: url.trimmed,
            stateFile: stateFile.trimmed.isEmpty ? nil : stateFile.trimmed,
            sync: SourceSyncPolicy(
                breakOnExisting: breakOnExisting.value,
                askOnExisting: askOnExisting.value,
                localIndexCache: localIndexCache.value
            ),
            adapter: SourceAdapter(
                kind: adapter.trimmed,
                extraArgs: args.isEmpty ? nil : args,
                minVersion: nil
            )
        ))
        dismiss()
    }
}
