import SwiftUI

/// C14 — the one screen that writes `config.yaml`.
///
/// Sidebar is the object list, centre is a `Form`, inspector is live
/// validation. Every write sends the SHA of the content that was read, so a
/// file that changed on disk since then produces its own conflict state with
/// two real choices, not a generic save error.
struct ConfigEditorView: View {
    @EnvironmentObject private var appState: AppState

    @State private var draft: MainConfig?
    @State private var selection: ConfigSelection = .defaults
    @State private var mode: ConfigViewMode = .form
    @State private var showingSourceEditor = false
    @State private var deletingSourceID: String?
    @State private var confirmingOverwrite = false

    var body: some View {
        content
            .sidebarContext(sidebarContext)
            .workspaceToolbar(title: "Advanced Config", subtitle: appState.configFile?.path) {
                Picker("View", selection: $mode) {
                    ForEach(ConfigViewMode.allCases) { option in
                        Text(option.label).tag(option)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 140)

                Button {
                    Task { await appState.loadConfigEditor(); loadDraft() }
                } label: {
                    Label("Reload", systemImage: "arrow.clockwise")
                }
                .help("Re-reads config.yaml from disk and discards unsaved edits.")
            }
            .workspaceInspector { inspector }
            .udlStatusBar { statusSummary } actions: { statusActions }
            .task {
                if appState.configFile == nil { await appState.loadConfigEditor() }
                loadDraft()
            }
            .sheet(isPresented: $showingSourceEditor) {
                SourceEditorView(existingIDs: draft?.sources.map(\.id) ?? []) { source in
                    draft?.sources.append(source)
                    selection = .source(source.id)
                }
            }
            .confirmationDialog(
                "Delete this source?",
                isPresented: Binding(
                    get: { deletingSourceID != nil },
                    set: { if !$0 { deletingSourceID = nil } }
                ),
                titleVisibility: .visible
            ) {
                Button("Delete source", role: .destructive) { deleteSource() }
                Button("Cancel", role: .cancel) { deletingSourceID = nil }
            } message: {
                Text("The source is removed from the draft. Nothing on disk changes until you save.")
            }
            .confirmationDialog(
                "Overwrite the file that changed on disk?",
                isPresented: $confirmingOverwrite,
                titleVisibility: .visible
            ) {
                Button("Overwrite with my version", role: .destructive) {
                    guard let draft else { return }
                    Task { _ = await appState.saveMainConfig(draft, overwritingExternalChanges: true) }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("udl will save without the content SHA, so the edits made outside UDL since this file was read are lost. Reload instead if you want to keep them.")
            }
    }

    // MARK: Content

    /// `BoundedContent` for the same reason every table-hosting screen uses it:
    /// a `Form` reports the ideal height of all its rows and does not shrink to
    /// a smaller proposal. On its own in the detail column it happens to fit;
    /// stack the C14 conflict banner above it and the column grows past the
    /// window, taking the sidebar, the toolbar and the status bar with it. Seen
    /// live the first time the conflict banner appeared.
    @ViewBuilder private var content: some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 0) {
                if let conflict = appState.configConflict {
                    conflictBanner(conflict)
                }
                if let draft = Binding($draft) {
                    switch mode {
                    case .form: form(draft)
                    case .yaml: yamlPane
                    }
                } else {
                    EmptyStateView(
                        title: "config.yaml",
                        kind: .working,
                        detail: "config.readFile has not returned yet."
                    )
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }

    /// C14 — nothing was written, and there are exactly two ways forward.
    private func conflictBanner(_ conflict: AppState.ConfigConflict) -> some View {
        Callout(
            title: "config.yaml changed on disk since UDL read it. Nothing was written.",
            detail: "udl compared the SHA of the content it read (\(short(conflict.expectedSHA256))) with what is there now (\(short(conflict.actualSHA256))) and refused the write. Reload to take the file on disk and lose your edits, or overwrite to keep yours and lose the external ones.",
            severity: .warn
        ) {
            HStack(spacing: 8) {
                Button("Reload from disk") {
                    Task { await appState.loadConfigEditor(); loadDraft() }
                }
                Button("Overwrite…", role: .destructive) { confirmingOverwrite = true }
                    .constrained(by: draft == nil ? "There is no draft to write." : nil)
                    .help("Saves without the content SHA, discarding the edits made outside UDL.")
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.top, 12)
    }

    @ViewBuilder private func form(_ draft: Binding<MainConfig>) -> some View {
        switch selection {
        case .defaults:
            ConfigDefaultsForm(defaults: draft.defaults)
        case .source(let id):
            if let index = draft.wrappedValue.sources.firstIndex(where: { $0.id == id }) {
                ConfigSourceForm(
                    source: draft.sources[index],
                    canMoveUp: index > 0,
                    canMoveDown: index < draft.wrappedValue.sources.count - 1,
                    onMoveUp: { move(from: index, to: index - 1) },
                    onMoveDown: { move(from: index, to: index + 1) },
                    onDuplicate: { duplicate(at: index) },
                    onDelete: { deletingSourceID = id }
                )
            } else {
                EmptyStateView(
                    title: "Source is gone",
                    kind: .empty,
                    detail: "This source is no longer in the draft. Pick another in the sidebar."
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    /// The mockup's YAML tab, with the one thing the protocol will not do.
    private var yamlPane: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                Callout(
                    title: isDirty
                        ? "This is config.yaml as it was read from disk — not your unsaved edits."
                        : "This is config.yaml as it was read from disk.",
                    detail: "udl has no method that canonicalises a config without writing it: config.validate returns only whether the config is valid, and config.writeFile returns the canonical text only after it has saved. Showing a rendered preview of the draft would mean this app guessing at udl's YAML output. Saving rewrites the file canonically, so hand-written comments and key order are not preserved.",
                    severity: isDirty ? .warn : .info
                )
                Text(appState.configFile?.content ?? "")
                    .font(Typography.monoBody)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(12)
                    .background(Theme.sunken, in: RoundedRectangle(cornerRadius: Metrics.cardRadius))
                    .overlay(
                        RoundedRectangle(cornerRadius: Metrics.cardRadius)
                            .stroke(Theme.separator, lineWidth: 0.5)
                    )
            }
            .padding(.horizontal, Metrics.contentPaddingHorizontal)
            .padding(.vertical, Metrics.contentPadding)
        }
    }

    // MARK: Draft edits

    private func move(from: Int, to: Int) {
        guard var config = draft, config.sources.indices.contains(from),
              config.sources.indices.contains(to) else { return }
        let source = config.sources.remove(at: from)
        config.sources.insert(source, at: to)
        draft = config
    }

    private func duplicate(at index: Int) {
        guard var config = draft, config.sources.indices.contains(index) else { return }
        let copy = config.sources[index].duplicated(existingIDs: config.sources.map(\.id))
        config.sources.insert(copy, at: index + 1)
        draft = config
        selection = .source(copy.id)
    }

    private func deleteSource() {
        guard let id = deletingSourceID, var config = draft else { return }
        config.sources.removeAll { $0.id == id }
        draft = config
        deletingSourceID = nil
        selection = config.sources.first.map { .source($0.id) } ?? .defaults
    }

    private func loadDraft() {
        draft = appState.configFile?.config
        if case .source(let id) = selection,
           draft?.sources.contains(where: { $0.id == id }) != true {
            selection = .defaults
        }
    }

    // MARK: Derived state

    private var isDirty: Bool {
        guard let draft, let saved = appState.configFile?.config else { return false }
        return draft != saved
    }

    /// Local checks plus whatever the backend last said. The backend's are kept
    /// separate so the inspector can say which side found what.
    private var issues: [ConfigIssue] {
        var all = draft.map(ConfigValidation.issues(in:)) ?? []
        all += appState.configProblems.map { ConfigIssue(message: $0, origin: .backend) }
        return all
    }

    private func short(_ sha: String) -> String {
        sha.isEmpty ? "not reported" : String(sha.prefix(12))
    }

    // MARK: Sidebar

    private var sidebarContext: SidebarContext? {
        guard let draft else { return nil }
        // The closure has to write through the projected binding: capturing the
        // view value and assigning to `selection` directly writes into the copy
        // the context was built from, and the click does nothing.
        let binding = $selection
        let problemIDs = Set(issues.compactMap(\.sourceID))
        var items = [
            SidebarContextItem(
                id: ConfigSelection.defaults.id,
                title: "Defaults",
                subtitle: "state_dir · threads · timeout"
            )
        ]
        items += draft.sources.map { source in
            SidebarContextItem(
                id: ConfigSelection.source(source.id).id,
                title: source.id.isEmpty ? "unnamed source" : source.id,
                subtitle: "\(source.adapter.kind) · \(source.enabled ? "enabled" : "disabled")",
                sourceType: source.type,
                lifecycle: problemIDs.contains(source.id) ? .failed : nil
            )
        }
        return SidebarContext(
            title: "Config objects",
            items: items,
            selectedID: selection.id,
            note: "Order here is the order udl runs sources in. Reordering, duplicating and deleting change the draft only.",
            select: { id in
                if let selected = ConfigSelection(id: id) { binding.wrappedValue = selected }
            }
        )
    }

    // MARK: Inspector

    @ViewBuilder private var inspector: some View {
        InspectorSection(title: "Validation") {
            if issues.isEmpty {
                HStack(spacing: 8) {
                    SeverityBadge(severity: .ok)
                    Text("No problems found.").font(Typography.control)
                    Spacer(minLength: 0)
                }
                ConstraintNote(
                    text: "The app checks what it can before saving; udl runs config.validate again on every save and is the authority."
                )
            } else {
                ForEach(issues) { issue in
                    HStack(alignment: .top, spacing: 8) {
                        SeverityBadge(severity: .error)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(issue.message)
                                .font(Typography.control)
                                .fixedSize(horizontal: false, vertical: true)
                            Text(issue.origin == .local ? "found by UDL" : "reported by udl")
                                .font(Typography.caption)
                                .foregroundStyle(Theme.textTertiary)
                        }
                        Spacer(minLength: 0)
                    }
                }
                ConstraintNote(
                    text: "Save stays disabled while any problem remains.",
                    severity: .warn,
                    symbol: "exclamationmark.triangle"
                )
            }
        }

        InspectorSection(title: "File") {
            if let file = appState.configFile {
                PathField(label: "Target", path: file.path)
                FieldRow("Version", "\(file.config.version)")
                FieldRow("Sources", "\(draft?.sources.count ?? file.config.sources.count)")
                // C14 — writes are guarded by the SHA of the content that was read.
                PathField(label: "content_sha256", path: file.contentSHA256)
                if appState.configConflict != nil {
                    ConstraintNote(
                        text: "This SHA no longer matches the file on disk, so udl is refusing writes until you reload or deliberately overwrite.",
                        severity: .warn,
                        symbol: "exclamationmark.triangle"
                    )
                } else {
                    ConstraintNote(
                        text: "Saving sends this SHA. If the file changed on disk since it was read, udl refuses the write instead of overwriting it."
                    )
                }
                if isDirty {
                    ConstraintNote(text: "The draft differs from the file that was read. Nothing is written until you save.")
                }
            } else {
                ConstraintNote(text: "config.readFile has not returned yet.")
            }
        }

        InspectorSection(title: "Other config files") {
            ForEach(featureConfigs, id: \.feature) { entry in
                ForEach(entry.paths, id: \.self) { path in
                    PathField(label: entry.feature, path: path)
                }
            }
            ConstraintNote(
                text: "Feature configs are edited in their own workflow. This screen writes config.yaml only, and it edits that one file rather than the merged view udl runs from."
            )
        }
    }

    private var featureConfigs: [(feature: String, paths: [String])] {
        (appState.initialization?.featureConfigPaths ?? [:])
            .sorted { $0.key < $1.key }
            .map { (feature: $0.key, paths: $0.value) }
    }

    // MARK: Status bar

    private var statusSummary: some View {
        SummaryLine {
            SummaryCount(value: draft?.sources.count ?? 0, noun: "sources", severity: .idle)
            Text("·")
            SummaryCount(
                value: issues.count,
                noun: "problems",
                severity: issues.isEmpty ? .idle : .error
            )
            if appState.configConflict != nil {
                Text("·")
                Text("Changed on disk — nothing written.").foregroundStyle(Theme.warn)
            } else if isDirty {
                Text("·")
                Text("Unsaved changes").foregroundStyle(Theme.accent)
            } else if let message = appState.configStatusMessage {
                Text("·")
                Text(message).foregroundStyle(Theme.textSecondary)
            }
        }
    }

    @ViewBuilder private var statusActions: some View {
        Button("Revert") { loadDraft() }
            .constrained(by: isDirty ? nil : "The draft matches the file that was read.")
        Button("Add source") { showingSourceEditor = true }
            .constrained(by: draft == nil ? "config.readFile has not returned yet." : nil)
        Button("Save") {
            guard let draft else { return }
            Task { _ = await appState.saveMainConfig(draft) }
        }
        .buttonStyle(.borderedProminent)
        .keyboardShortcut("s", modifiers: .command)
        .constrained(by: saveBlockedReason)
        .help(saveBlockedReason ?? "Validates, then writes canonical YAML guarded by the content SHA.")
    }

    /// Names the first thing standing between the draft and a write, so the
    /// disabled Save states its reason rather than only carrying a tooltip.
    private var saveBlockedReason: String? {
        if draft == nil { return "config.readFile has not returned yet." }
        if !issues.isEmpty { return "Fix the \(issues.count) validation problem(s) first." }
        if !isDirty { return "The draft matches the file on disk." }
        return nil
    }
}
