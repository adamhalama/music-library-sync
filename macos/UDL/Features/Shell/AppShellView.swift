import SwiftUI

/// The application shell: sidebar · content · per-screen inspector, with a
/// shell-level recovery banner above everything.
struct AppShellView: View {
    @EnvironmentObject private var appState: AppState
    @StateObject private var sidebarContext = SidebarContextStore()

    var body: some View {
        NavigationSplitView {
            SidebarView()
                .environmentObject(sidebarContext)
        } detail: {
            VStack(spacing: 0) {
                recoveryBanner
                startupBanner
                workspace
            }
            .workspaceContent()
        }
        .environmentObject(sidebarContext)
    }

    /// C15 — the backend can exit or EOF at any time. The banner is
    /// shell-level because the loss is not specific to one screen.
    @ViewBuilder private var recoveryBanner: some View {
        if let recovery = appState.backendRecovery {
            Callout(
                title: recovery.message,
                detail: recovery.interrupted.isEmpty
                    ? "Nothing was running when the connection ended."
                    : "Not resumed: \(recovery.interrupted.map(\.rawValue).joined(separator: ", ")). Re-run to continue.",
                severity: .error
            ) {
                HStack(spacing: 8) {
                    Button("Dismiss") { appState.dismissBackendRecovery() }
                    Button("Restart Backend") { Task { await appState.restart() } }
                        .buttonStyle(.borderedProminent)
                }
            }
            .padding(.horizontal, Metrics.contentPaddingHorizontal)
            .padding(.top, 12)
        }
    }

    @ViewBuilder private var startupBanner: some View {
        if let startup = appState.startupAttention, let attention = startup.attention {
            Callout(
                title: attention.headline,
                detail: attention.summaryText,
                severity: attention.severity == "blocked" ? .error : .warn
            ) {
                Button(attention.primaryActionLabel) { appState.destination = .credentials }
            }
            .padding(.horizontal, Metrics.contentPaddingHorizontal)
            .padding(.top, 12)
        }
    }

    @ViewBuilder private var workspace: some View {
        switch appState.destination {
        case .onboarding: OnboardingView()
        case .home, nil: HomeView()
        case .sync: SyncView()
        case .freeDL: FreeDLView()
        case .rekordbox: RekordboxView()
        case .playlists: PlaylistsView()
        case .doctor: DoctorView()
        case .credentials: CredentialsView()
        case .config: ConfigEditorView()
        }
    }
}
