import SwiftUI

/// The confirm and masked-input prompts, which stay real modals because they
/// interrupt a run rather than being a place the user works.
///
/// C1 — `ui.selectRows` is deliberately *not* handled here. The plan is docked
/// into the Sync workspace (`SyncPlanView`), because it is the screen where the
/// user does the actual work of the run.
struct PendingPromptView: View {
    @EnvironmentObject private var appState: AppState
    @State private var input = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text(promptTitle)
                .font(Typography.sectionTitle)
            // C3 — Go is blocked on this request until the app replies.
            WaitingBanner(
                title: "Backend paused — waiting for your answer.",
                detail: "The run cannot continue until this prompt is answered or cancelled."
            )
            .clipShape(RoundedRectangle(cornerRadius: Metrics.cardRadius))
            Text(promptText)
                .font(Typography.control)
                .foregroundStyle(Theme.textSecondary)
                .textSelection(.enabled)

            if appState.pendingPrompt?.request.kind == .input {
                SecureField("Replacement value", text: $input)
                    .textFieldStyle(.roundedBorder)
            }

            HStack {
                Button("Cancel", role: .cancel) {
                    input.removeAll(keepingCapacity: false)
                    Task { await appState.cancelPrompt() }
                }
                Spacer()
                Button(primaryLabel) {
                    let result = promptResult
                    input.removeAll(keepingCapacity: false)
                    Task { await appState.answerPrompt(result: result) }
                }
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(28)
        .frame(width: 480)
        .interactiveDismissDisabled()
    }

    private var promptTitle: String {
        switch appState.pendingPrompt?.request.kind {
        case .confirm: "Confirmation requested"
        case .input: "Secure input requested"
        // Docked in SyncPlanView; never reached from this sheet.
        case .selectRows: "Plan selection requested"
        case nil: "Backend request"
        }
    }

    private var promptText: String {
        guard case .object(let params) = appState.pendingPrompt?.request.params else {
            return "The backend is waiting for a response."
        }
        if case .string(let prompt) = params["prompt"] { return prompt }
        return "Review the request and continue. Detailed row selection is available in the Sync workflow."
    }

    private var primaryLabel: String {
        appState.pendingPrompt?.request.kind == .confirm ? "Continue" : "Submit"
    }

    private var promptResult: JSONValue {
        switch appState.pendingPrompt?.request.kind {
        case .confirm:
            return .object(["confirmed": .bool(true)])
        case .input:
            return .object(["value": .string(input)])
        case .selectRows:
            return .null
        case nil:
            return .null
        }
    }
}
