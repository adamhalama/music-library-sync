import SwiftUI

struct CredentialsView: View {
    @EnvironmentObject private var appState: AppState
    @State private var editing: CredentialKind?
    @State private var clearing: CredentialKind?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 26) {
                header
                Text("Existing values are never loaded. Replace fields always begin empty.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .padding(14)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.orange.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))

                LazyVStack(spacing: 12) {
                    ForEach(appState.credentials) { credential in
                        credentialRow(credential)
                    }
                }
            }
            .padding(34)
            .frame(maxWidth: 920, alignment: .leading)
        }
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
        }
    }

    private var header: some View {
        HStack(alignment: .bottom) {
            VStack(alignment: .leading, spacing: 7) {
                Text("SECURE INPUT BAY")
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .tracking(1.8)
                    .foregroundStyle(.orange)
                Text("Credentials")
                    .font(.system(size: 36, weight: .heavy, design: .rounded))
                Text("Presence and health only. Secret material stays in Keychain.")
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button {
                Task { await appState.refreshCredentials() }
            } label: {
                Label("Refresh", systemImage: "arrow.clockwise")
            }
            .buttonStyle(.bordered)
            .disabled(appState.isLoadingCredentials)
        }
    }

    private func credentialRow(_ credential: CredentialStatus) -> some View {
        HStack(spacing: 16) {
            Image(systemName: credential.health == "available" ? "key.fill" : "key.slash")
                .font(.title2)
                .foregroundStyle(healthColor(credential.health))
                .frame(width: 34)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(credential.title).font(.headline)
                    Text(credential.health.replacingOccurrences(of: "_", with: " ").uppercased())
                        .font(.caption2.bold().monospaced())
                        .foregroundStyle(healthColor(credential.health))
                }
                Text(credential.summary)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                if let failure = credential.lastFailureMessage, !failure.isEmpty {
                    Text(failure).font(.caption).foregroundStyle(.orange)
                }
            }
            Spacer()
            Button("Replace") { editing = credential.kind }
                .buttonStyle(.borderedProminent)
            Button("Clear", role: .destructive) { clearing = credential.kind }
                .buttonStyle(.bordered)
        }
        .padding(18)
        .background(.white.opacity(0.04), in: RoundedRectangle(cornerRadius: 13))
        .overlay(RoundedRectangle(cornerRadius: 13).stroke(.white.opacity(0.08)))
    }

    private func healthColor(_ health: String) -> Color {
        switch health {
        case "available", "external_override": .green
        case "needs_refresh": .red
        default: .orange
        }
    }
}

private struct CredentialEditor: View {
    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss
    let kind: CredentialKind
    @State private var value = ""
    @State private var clientID = ""
    @State private var clientSecret = ""
    @State private var saving = false

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("Replace credential").font(.title2.bold())
            Text(label).foregroundStyle(.secondary)

            if kind == .spotifyApp {
                TextField("Spotify client ID", text: $clientID)
                    .textFieldStyle(.roundedBorder)
                SecureField("Spotify client secret", text: $clientSecret)
                    .textFieldStyle(.roundedBorder)
            } else {
                SecureField(kind == .deemixARL ? "Deezer ARL" : "SoundCloud client ID", text: $value)
                    .textFieldStyle(.roundedBorder)
            }

            HStack {
                Button("Cancel", role: .cancel) {
                    clearMemory()
                    dismiss()
                }
                Spacer()
                Button("Save replacement") {
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
                .disabled(saving || !valid)
            }
        }
        .padding(26)
        .frame(width: 460)
        .interactiveDismissDisabled(saving)
        .onDisappear { clearMemory() }
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
