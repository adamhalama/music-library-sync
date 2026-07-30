import SwiftUI

private enum ControlRoomTheme {
    static let canvas = Color(red: 0.055, green: 0.063, blue: 0.071)
    static let panel = Color(red: 0.085, green: 0.095, blue: 0.105)
    static let line = Color.white.opacity(0.09)
    static let signal = Color(red: 0.98, green: 0.69, blue: 0.25)
    static let healthy = Color(red: 0.37, green: 0.81, blue: 0.57)
}

struct DashboardView: View {
    @EnvironmentObject private var appState: AppState

    var body: some View {
        NavigationSplitView {
            VStack(spacing: 0) {
                brand
                List(AppState.Destination.allCases, selection: $appState.destination) { destination in
                    Label(destination.rawValue, systemImage: icon(for: destination))
                        .tag(destination)
                        .foregroundStyle(isAvailable(destination) ? .primary : .secondary)
                }
                .scrollContentBackground(.hidden)
                BackendStatusView(backend: appState.backend, version: appState.backendVersion)
            }
            .background(ControlRoomTheme.panel)
            .navigationSplitViewColumnWidth(min: 220, ideal: 246)
        } detail: {
            ZStack {
                ControlRoomTheme.canvas.ignoresSafeArea()
                VStack(spacing: 0) {
                    if let startup = appState.startupAttention,
                       let attention = startup.attention {
                        HStack {
                            Image(systemName: attention.severity == "blocked" ? "exclamationmark.octagon.fill" : "exclamationmark.triangle.fill")
                            VStack(alignment: .leading) {
                                Text(attention.headline).font(.headline)
                                Text(attention.summaryText).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Button(attention.primaryActionLabel) { appState.destination = .credentials }
                        }
                        .padding(12)
                        .background(Color.orange.opacity(0.12))
                    } else if appState.startupAttention?.status == "ready" {
                        HStack {
                            Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                            Text("Startup checks ready").font(.caption.bold().monospaced())
                            Spacer()
                        }
                        .padding(.horizontal, 14)
                        .frame(height: 34)
                        .background(Color.green.opacity(0.08))
                    }
                    detail
                }
            }
        }
        .preferredColorScheme(.dark)
        .tint(ControlRoomTheme.signal)
    }

    private var brand: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("UDL")
                .font(.system(size: 28, weight: .black, design: .rounded))
                .tracking(3)
            Text("LIBRARY CONTROL ROOM")
                .font(.system(size: 9, weight: .semibold, design: .monospaced))
                .foregroundStyle(ControlRoomTheme.signal)
                .tracking(1.4)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(22)
        .overlay(alignment: .bottom) { Divider().overlay(ControlRoomTheme.line) }
    }

    @ViewBuilder private var detail: some View {
        switch appState.destination {
        case .onboarding: OnboardingView()
        case .doctor: DoctorView()
        case .credentials: CredentialsView()
        case .sync: SyncView()
        case .playlists: PlaylistsView()
        case .freeDL: FreeDLView()
        case .rekordbox: RekordboxView()
        case .config: ConfigEditorView()
        case nil:
            DoctorView()
        }
    }

    private func icon(for destination: AppState.Destination) -> String {
        switch destination {
        case .onboarding: "sparkles"
        case .doctor: "waveform.path.ecg"
        case .credentials: "key.horizontal"
        case .sync: "arrow.triangle.2.circlepath"
        case .playlists: "music.note.list"
        case .freeDL: "arrow.down.circle"
        case .rekordbox: "square.stack.3d.up"
        case .config: "slider.horizontal.3"
        }
    }

    private func isAvailable(_ destination: AppState.Destination) -> Bool {
        true
    }
}

private struct BackendStatusView: View {
    @ObservedObject var backend: AgentProcess
    let version: String

    var body: some View {
        HStack(spacing: 10) {
            Circle()
                .fill(backend.state == .running ? ControlRoomTheme.healthy : ControlRoomTheme.signal)
                .frame(width: 8, height: 8)
                .shadow(color: backend.state == .running ? ControlRoomTheme.healthy : ControlRoomTheme.signal, radius: 5)
            VStack(alignment: .leading, spacing: 2) {
                Text(backend.state.label)
                    .font(.system(size: 11, weight: .semibold, design: .monospaced))
                Text("backend \(version)")
                    .font(.caption2.monospaced())
                    .foregroundStyle(.secondary)
            }
            Spacer()
        }
        .padding(18)
        .overlay(alignment: .top) { Divider().overlay(ControlRoomTheme.line) }
    }
}

private struct PlannedFeatureView: View {
    let title: String
    var body: some View {
        ContentUnavailableView(
            "\(title) is queued",
            systemImage: "dial.medium",
            description: Text("The backend contract is ready. This surface arrives in the next parity slice.")
        )
    }
}
