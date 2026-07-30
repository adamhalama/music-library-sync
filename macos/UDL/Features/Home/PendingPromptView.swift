import SwiftUI

struct PendingPromptView: View {
    @EnvironmentObject private var appState: AppState
    @State private var input = ""

    var body: some View {
        if let prompt = appState.pendingPrompt, prompt.request.kind == .selectRows {
            PlanSelectionPromptView(request: prompt.request)
        } else {
            simplePrompt
        }
    }

    private var simplePrompt: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text(promptTitle)
                .font(.title2.bold())
            Text(promptText)
                .foregroundStyle(.secondary)
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

private struct PlanSelectionPromptView: View {
    @EnvironmentObject private var appState: AppState
    let request: UIRequest

    @State private var params: SelectRowsParams?
    @State private var selected: Set<Int> = []
    @State private var order: DownloadOrder = .newestFirst
    @State private var window: PlanWindow = .first
    @State private var filter = PlanRowFilter.all
    @State private var cursor: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text(params?.sourceID ?? "Plan selection")
                        .font(.title2.bold())
                    if let details = params?.details {
                        Text("\(details.sourceType) · \(details.adapter) · \(details.dryRun ? "dry run" : "live")")
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
                Picker("Filter", selection: $filter) {
                    ForEach(PlanRowFilter.allCases) { Text($0.label).tag($0) }
                }
                .frame(width: 170)
            }

            if let details = params?.details {
                Grid(alignment: .leading, horizontalSpacing: 20, verticalSpacing: 4) {
                    GridRow { Text("Target").foregroundStyle(.secondary); Text(details.targetDir).textSelection(.enabled) }
                    GridRow { Text("State").foregroundStyle(.secondary); Text(details.stateFile).textSelection(.enabled) }
                    GridRow { Text("URL").foregroundStyle(.secondary); Text(details.url).textSelection(.enabled) }
                }
                .font(.caption.monospaced())
            }

            Table(filteredRows, selection: $cursor) {
                TableColumn("") { row in
                    Toggle(
                        "",
                        isOn: Binding(
                            get: { selected.contains(row.index) },
                            set: { value in
                                guard row.toggleable else { return }
                                if value { selected.insert(row.index) } else { selected.remove(row.index) }
                            }
                        )
                    )
                    .labelsHidden()
                    .disabled(!row.toggleable)
                }
                .width(28)
                TableColumn("Track", value: \.title)
                TableColumn("State") { row in
                    Text(row.status.replacingOccurrences(of: "_", with: " "))
                        .font(.caption.monospaced())
                }
                .width(min: 120, ideal: 150)
            }
            .frame(minHeight: 310)
            .onChange(of: cursor) { _, newCursor in
                guard let params else { return }
                appState.rememberPlanSelection(
                    params,
                    selectedIndices: selected,
                    cursor: newCursor
                )
            }

            HStack {
                Text("\(selected.count) selected")
                    .font(.caption.bold().monospaced())
                Spacer()
                Picker("Order", selection: $order) {
                    ForEach(DownloadOrder.allCases) { Text($0.label).tag($0) }
                }
                .frame(width: 190)
                Picker("Window", selection: $window) {
                    ForEach(PlanWindow.allCases) { Text($0.label).tag($0) }
                }
                .frame(width: 150)
                .disabled(params?.details.adapter != "deemix")
            }

            HStack {
                Button("Cancel", role: .cancel) {
                    Task { await appState.cancelPrompt() }
                }
                Spacer()
                if window != params?.planWindow {
                    Button("Rebuild plan") { submit(rebuild: true) }
                }
                Button("Continue") { submit(rebuild: false) }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(24)
        .frame(width: 900, height: 610)
        .interactiveDismissDisabled()
        .task(id: requestID) { decodeRequest() }
    }

    private var requestID: String { String(describing: request.id) }

    private var filteredRows: [PlanRow] {
        guard let params else { return [] }
        return params.rows.filter(filter.includes)
    }

    private func decodeRequest() {
        guard let data = try? JSONEncoder.agent.encode(request.params),
              let decoded = try? JSONDecoder.agent.decode(SelectRowsParams.self, from: data) else {
            appState.alertMessage = "The backend sent an invalid plan-selection request."
            return
        }
        params = decoded
        selected = appState.initialPlanSelection(decoded)
        cursor = appState.rememberedPlanCursor(sourceID: decoded.sourceID, rows: decoded.rows)
        order = decoded.downloadOrder
        window = decoded.planWindow
    }

    private func submit(rebuild: Bool) {
        if let params {
            appState.rememberPlanSelection(
                params,
                selectedIndices: selected,
                cursor: cursor
            )
        }
        let result = SelectRowsResult(
            selectedIndices: rebuild ? [] : selected.sorted(),
            downloadOrder: order,
            canceled: false,
            rebuild: rebuild,
            planWindow: window
        )
        Task { await appState.answerPlanSelection(result, request: request) }
    }
}

private enum PlanRowFilter: String, CaseIterable, Identifiable {
    case all
    case willSync
    case missingNew
    case knownGap
    case alreadyHave
    var id: String { rawValue }
    var label: String {
        switch self {
        case .all: "All"
        case .willSync: "Will sync"
        case .missingNew: "Missing new"
        case .knownGap: "Known gap"
        case .alreadyHave: "Already have"
        }
    }

    func includes(_ row: PlanRow) -> Bool {
        switch self {
        case .all: true
        case .willSync: row.toggleable
        case .missingNew: row.status == "missing_new"
        case .knownGap: row.status == "missing_known_gap"
        case .alreadyHave: row.status == "already_downloaded"
        }
    }
}
