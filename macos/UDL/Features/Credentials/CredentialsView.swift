import SwiftUI

/// A status board rather than a form. Each credential shows its health, where
/// the value the backend actually uses comes from, which configured sources
/// need it, and exactly one action.
///
/// C12 — `credentials.list` is metadata-only. No editor on this screen can
/// prefill, and every surface says so.
struct CredentialsView: View {
    @EnvironmentObject private var appState: AppState
    @State private var selection: CredentialKind?
    @State private var editing: CredentialKind?
    @State private var clearing: CredentialKind?

    var body: some View {
        content
            .workspaceToolbar(title: "Credentials", subtitle: "credentials.list · macOS Keychain") {
                Button {
                    Task { await appState.refreshCredentials() }
                } label: {
                    if appState.isLoadingCredentials {
                        Label { Text("Reloading…") } icon: { ProgressView().controlSize(.small) }
                    } else {
                        Label("Reload", systemImage: "arrow.clockwise")
                    }
                }
                .disabled(appState.isLoadingCredentials)
            }
            .workspaceInspector { inspector }
            .udlStatusBar { statusSummary } actions: { statusActions }
            .sidebarContext(sidebarContext)
            .sheet(item: $editing) { kind in
                CredentialEditor(kind: kind)
                    .environmentObject(appState)
            }
            .confirmationDialog(
                "Clear this credential from macOS Keychain?",
                isPresented: Binding(
                    get: { clearing != nil },
                    set: { if !$0 { clearing = nil } }
                ),
                titleVisibility: .visible
            ) {
                Button("Clear credential", role: .destructive) {
                    guard let kind = clearing else { return }
                    clearing = nil
                    Task { await appState.clearCredential(kind) }
                }
                Button("Cancel", role: .cancel) { clearing = nil }
            } message: {
                Text("Sources that need it are skipped with a clear error instead of failing mid-run. Nothing else is changed.")
            }
            .onChange(of: appState.credentials.map(\.kind)) { _, kinds in
                if selection == nil || !kinds.contains(where: { $0 == selection }) {
                    selection = kinds.first
                }
            }
            .task { if selection == nil { selection = appState.credentials.first?.kind } }
    }

    // MARK: Content

    /// `BoundedContent` because this screen hosts a `Table`. Three rows would
    /// not trigger the unbounded-table bug today; using the shape anyway is
    /// what stops it being a latent one.
    private var content: some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 0) {
                // A Keychain refusal is this screen's own outcome. It used to be
                // a modal that named neither the credential nor the screen.
                if let status = appState.credentialStatus, status.severity != .info {
                    Callout(title: status.message, severity: status.severity)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.top, 10)
                }
                if appState.credentials.isEmpty {
                    EmptyStateView(
                        title: "Credentials",
                        kind: appState.isLoadingCredentials ? .working : .notRun(action: "reload to list them")
                    )
                } else {
                    credentialTable
                    precedenceCallout
                }
            }
        }
    }

    private var credentialTable: some View {
        Table(appState.credentials, selection: $selection) {
            TableColumn("Credential") { credential in
                VStack(alignment: .leading, spacing: 1) {
                    Text(credential.title).font(Typography.control)
                    // C12 — the value is never read back, so the second line is
                    // the wire key, not a masked secret.
                    Text(credential.kind.rawValue)
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textTertiary)
                }
            }

            TableColumn("Health") { credential in
                StatusPill(
                    title: credential.healthLabel,
                    severity: appState.credentialSeverity(credential)
                )
            }
            .width(min: 120, ideal: 132)

            TableColumn("Used by") { credential in
                usedBy(credential)
            }
            .width(min: 140, ideal: 178)

            TableColumn("Stored in") { credential in
                MonoValue(
                    text: credential.storageLabel,
                    severity: credential.isExternallyOverridden ? .warn : .idle
                )
            }
            .width(min: 120, ideal: 150)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
    }

    @ViewBuilder
    private func usedBy(_ credential: CredentialStatus) -> some View {
        let consumers = credentialConsumers(credential.kind, sources: appState.syncSources)
        if appState.syncSources.isEmpty {
            Text("sources.capabilities not loaded")
                .font(Typography.caption)
                .foregroundStyle(Theme.textTertiary)
        } else if consumers.isEmpty {
            Text("No configured source")
                .font(Typography.caption)
                .foregroundStyle(Theme.textTertiary)
        } else {
            HStack(spacing: 5) {
                ForEach(consumers, id: \.self) { sourceID in
                    Text(sourceID)
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textSecondary)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .background(Theme.fill, in: RoundedRectangle(cornerRadius: 4))
                }
            }
        }
    }

    private var precedenceCallout: some View {
        VStack(alignment: .leading, spacing: 10) {
            // C12 stated on the screen, not only inside the editor.
            Callout(
                title: "The existing value is never loaded or shown.",
                detail: "credentials.list reports presence and health only. Every editor starts empty and replaces the stored secret outright.",
                severity: .info
            )
            // C13 — precedence, so the screen never implies the Keychain entry
            // is the one actually running.
            Callout(
                title: "Precedence: environment variable → macOS Keychain → ~/.spotdl/config.json.",
                detail: "When something outside Keychain wins, the row is marked and its action becomes “Move to Keychain”.",
                severity: .warn
            )
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.chrome)
        .overlay(alignment: .top) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }

    // MARK: Sidebar

    private var sidebarContext: SidebarContext? {
        guard !appState.credentials.isEmpty else { return nil }
        let items = appState.credentials.map { credential in
            SidebarContextItem(
                id: credential.kind.rawValue,
                title: credential.title,
                subtitle: credential.storageLabel
            )
        }
        return SidebarContext(
            title: "Credentials",
            items: items,
            selectedID: selection?.rawValue,
            note: nil,
            select: { id in selection = CredentialKind(rawValue: id) }
        )
    }

    // MARK: Inspector

    @ViewBuilder private var inspector: some View {
        if let credential = selectedCredential {
            let severity = appState.credentialSeverity(credential)
            InspectorSection(title: "Selected") {
                HStack(spacing: 7) {
                    SeverityBadge(severity: severity)
                    Text(credential.title).font(Typography.cardTitle)
                    Spacer(minLength: 0)
                }
                FieldRow(label: "Health") {
                    StatusPill(title: credential.healthLabel, severity: severity)
                }
                FieldRow("Stored in", credential.storageLabel)
                Text(credential.summary)
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                if let failure = credential.lastFailureMessage, !failure.isEmpty {
                    ConstraintNote(text: failure, severity: .warn, symbol: "exclamationmark.triangle")
                }
            }

            InspectorSection(title: "Used by") {
                let consumers = credentialConsumers(credential.kind, sources: appState.syncSources)
                if consumers.isEmpty {
                    ConstraintNote(
                        text: appState.syncSources.isEmpty
                            ? "sources.capabilities has not loaded, so udl cannot say which sources need this."
                            : "No configured source uses this credential right now."
                    )
                } else {
                    ForEach(consumers, id: \.self) { FieldRow("Source", $0) }
                }
                ConstraintNote(text: "Derived from sources.capabilities. credentials.list reports no workflow list of its own.")
            }

            InspectorSection(title: "Environment override") {
                ForEach(credential.environmentVariables, id: \.self) { variable in
                    MonoValue(text: variable, severity: credential.isExternallyOverridden ? .warn : .idle)
                }
                if credential.isExternallyOverridden {
                    ConstraintNote(
                        text: "Something outside Keychain is supplying this value, so the Keychain entry is not what the backend uses. “Move to Keychain” writes the entry; the override still wins until it is unset.",
                        severity: .warn
                    )
                } else {
                    ConstraintNote(text: "Set this and it wins over Keychain for the running backend.")
                }
            }

            InspectorSection(title: "Actions") {
                Button(credential.actionLabel) { editing = credential.kind }
                    .buttonStyle(.borderedProminent)
                    .frame(maxWidth: .infinity)
                Button("Clear from Keychain", role: .destructive) { clearing = credential.kind }
                    .frame(maxWidth: .infinity)
                    .constrained(
                        by: credential.storageSource.isEmpty
                            ? "credentials.list reports no stored value for this credential."
                            : nil
                    )
                if credential.isExternallyOverridden {
                    ConstraintNote(
                        text: "Clearing removes the Keychain entry only. The override above keeps supplying the value until it is unset.",
                        severity: .warn
                    )
                }
            }
        } else {
            InspectorSection(title: "Selected") {
                ConstraintNote(text: "Select a credential to see where its value comes from.")
            }
        }
    }

    private var selectedCredential: CredentialStatus? {
        appState.credentials.first { $0.kind == selection }
    }

    // MARK: Status bar

    private var statusSummary: some View {
        let overridden = appState.credentials.filter(\.isExternallyOverridden).count
        return SummaryLine {
            SummaryCount(
                value: appState.attention.unhealthyCredentials,
                noun: "need attention",
                severity: appState.attention.unhealthyCredentials > 0 ? .error : .idle
            )
            Text("·")
            SummaryCount(value: overridden, noun: "external override", severity: overridden > 0 ? .warn : .idle)
            Text("·")
            SummaryCount(value: appState.credentials.count, noun: "tracked", severity: .idle)
        }
    }

    @ViewBuilder private var statusActions: some View {
        Button("Re-run checks") {
            appState.destination = .doctor
            Task { await appState.refreshDoctor() }
        }
        if let credential = selectedCredential {
            Button(credential.actionLabel) { editing = credential.kind }
                .buttonStyle(.borderedProminent)
        }
    }
}

/// C12 — the editor buffer genuinely starts empty and is wiped on the way out.
/// This is the guard against the ARL double-paste bug recorded in `AGENTS.md`.
private struct CredentialEditor: View {
    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss
    let kind: CredentialKind
    @State private var value = ""
    @State private var clientID = ""
    @State private var clientSecret = ""
    @State private var showValue = false
    @State private var saving = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(label).font(Typography.sectionTitle)
            Text("udl stores this in macOS Keychain and reads it only when a matching source runs. It is never written to YAML and never logged.")
                .font(Typography.caption)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)

            ConstraintNote(
                text: "The existing value is never loaded or shown. What you type replaces it outright."
            )

            if kind == .spotifyApp {
                secretField("Spotify client ID", text: $clientID)
                secretField("Spotify client secret", text: $clientSecret)
            } else {
                secretField(kind == .deemixARL ? "Deezer ARL" : "SoundCloud client ID", text: $value)
            }

            Toggle("Show value", isOn: $showValue)
                .toggleStyle(.checkbox)
                .font(Typography.control)

            HStack {
                Button("Cancel", role: .cancel) {
                    clearMemory()
                    dismiss()
                }
                Spacer()
                Button("Save to Keychain") {
                    saving = true
                    Task {
                        let saved = await appState.saveCredential(
                            kind: kind,
                            value: credentialReplacementValue(submitted: value),
                            clientID: credentialReplacementValue(submitted: clientID),
                            clientSecret: credentialReplacementValue(submitted: clientSecret)
                        )
                        clearMemory()
                        saving = false
                        if saved { dismiss() }
                    }
                }
                .buttonStyle(.borderedProminent)
                .constrained(by: saving ? "Saving to the Keychain…" : (valid ? nil : "Enter the replacement value first."))
            }
        }
        .padding(26)
        .frame(width: 480)
        .interactiveDismissDisabled(saving)
        .onDisappear { clearMemory() }
    }

    @ViewBuilder
    private func secretField(_ prompt: String, text: Binding<String>) -> some View {
        if showValue {
            TextField(prompt, text: text)
                .textFieldStyle(.roundedBorder)
                .font(Typography.monoBody)
        } else {
            SecureField(prompt, text: text)
                .textFieldStyle(.roundedBorder)
        }
    }

    private var valid: Bool {
        kind == .spotifyApp
            ? !clientID.trimmingCharacters(in: .whitespaces).isEmpty &&
                !clientSecret.trimmingCharacters(in: .whitespaces).isEmpty
            : !value.trimmingCharacters(in: .whitespaces).isEmpty
    }

    private var label: String {
        switch kind {
        case .soundCloudClientID: "SoundCloud client ID"
        case .deemixARL: "Deezer ARL"
        case .spotifyApp: "Spotify application credentials"
        case .navidromePassword: "Navidrome account password"
        }
    }

    private func clearMemory() {
        value.removeAll(keepingCapacity: false)
        clientID.removeAll(keepingCapacity: false)
        clientSecret.removeAll(keepingCapacity: false)
    }
}

// Replacement is deliberately a pure pass-through from an initially empty
// editor buffer. Existing presence metadata is never part of the value path.
func credentialReplacementValue(submitted: String) -> String {
    submitted
}
