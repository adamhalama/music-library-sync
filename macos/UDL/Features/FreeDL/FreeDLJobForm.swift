import SwiftUI

/// The Free DL inspector's job form. Editing here writes `freedl.yaml` through
/// `freedl.config.write`, which is the same scoped, validated, canonically
/// re-marshalled path the config screen uses — the app never edits the file
/// text itself.
///
/// The mockup's controls are not all buildable as drawn; where the config
/// schema disagrees, the schema wins and the control states why:
///
/// * **No "Unlimited" switch.** `internal/freedl/config.go` normalises a job's
///   `plan_limit: 0` to the *defaults* plan limit, so 0 means "use 50", not
///   "no limit". Offering an unlimited toggle here would silently mean 50.
/// * **Target format is auto / wav / mp3-320 / aac-256.** `validTargetFormat`
///   rejects the mockup's FLAC.
struct FreeDLJobForm: View {
    @EnvironmentObject private var appState: AppState

    let config: FreeDLConfigResult
    let jobID: String

    @State private var draft: FreeDLJob?
    @State private var isSaving = false
    @State private var saveError: String?

    private static let targetFormats = ["auto", "wav", "mp3-320", "aac-256"]

    var body: some View {
        if let job = draft ?? config.config.jobs.first(where: { $0.id == jobID }) {
            form(job)
                .onChange(of: jobID, initial: true) { _, _ in
                    draft = config.config.jobs.first(where: { $0.id == jobID })
                    saveError = nil
                }
        } else {
            ConstraintNote(text: "Select a Free DL job in the sidebar to edit it.")
        }
    }

    @ViewBuilder
    private func form(_ job: FreeDLJob) -> some View {
        let binding = Binding(get: { draft ?? job }, set: { draft = $0 })

        InspectorSection(title: "Job settings") {
            Toggle("Enabled", isOn: binding.enabled)
                .toggleStyle(.switch)
                .controlSize(.small)

            FieldRow(label: "Plan limit") {
                TextField("", value: binding.planLimit, format: .number)
                    .labelsHidden()
                    .frame(width: 64)
                    .multilineTextAlignment(.trailing)
            }
            ConstraintNote(
                text: "0 means \"use the Free DL default of \(config.config.defaults.planLimit)\", not unlimited. Unlike the sync plan limit, a Free DL job has no unlimited value."
            )

            Picker("Download order", selection: binding.downloadOrder) {
                Text("Oldest first").tag("oldest_first")
                Text("Newest first").tag("newest_first")
            }

            Picker("Target format", selection: binding.targetFormat) {
                ForEach(Self.targetFormats, id: \.self) { Text($0).tag($0) }
            }
            ConstraintNote(text: "udl validates this against auto, wav, mp3-320 and aac-256. There is no FLAC target.")

            FieldRow(label: "Min match score") {
                TextField("", value: binding.minMatchScore, format: .number)
                    .labelsHidden()
                    .frame(width: 64)
                    .multilineTextAlignment(.trailing)
            }
            FieldRow(label: "Ambiguity gap") {
                TextField("", value: binding.ambiguityGap, format: .number)
                    .labelsHidden()
                    .frame(width: 64)
                    .multilineTextAlignment(.trailing)
            }
            FieldRow(label: "Replace limit") {
                TextField("", value: binding.replaceLimit, format: .number)
                    .labelsHidden()
                    .frame(width: 64)
                    .multilineTextAlignment(.trailing)
            }
            ConstraintNote(
                text: "Rows below the minimum match score stay visible and inspectable, but udl never selects them for promotion."
            )

            Toggle("Apply promotions automatically", isOn: binding.applyPromotions)
                .toggleStyle(.switch)
                .controlSize(.small)

            if let saveError {
                ConstraintNote(text: saveError, severity: .error, symbol: "exclamationmark.triangle")
            }

            HStack {
                Button("Revert") {
                    draft = config.config.jobs.first(where: { $0.id == jobID })
                    saveError = nil
                }
                .disabled(!isDirty(job))
                Spacer()
                Button("Save freedl.yaml") { save() }
                    .buttonStyle(.borderedProminent)
                    .disabled(!isDirty(job) || isSaving)
            }
            if !isDirty(job) {
                ConstraintNote(text: "Nothing to save — this form matches freedl.yaml.")
            }
        }

        InspectorSection(title: "Directories") {
            PathField(label: "Source", path: job.sourceURL)
            PathField(label: "Library", path: job.libraryDir)
            PathField(label: "Buffer", path: job.bufferDir)
            PathField(label: "Backup", path: job.backupDir)
            PathField(label: "Logs", path: job.logDir)
            ConstraintNote(
                text: "Capture writes only into Buffer. Promotion copies the original into Backup before it replaces anything in Library."
            )
        }

        InspectorSection(title: "Feature config") {
            PathField(label: "File", path: config.path)
            FieldRow("Jobs", "\(config.config.jobs.count)")
            FieldRow("Enabled", "\(config.config.jobs.filter(\.enabled).count)")
            ConstraintNote(text: "Saving re-marshals the whole file canonically through freedl.config.write.")
        }
    }

    private func isDirty(_ job: FreeDLJob) -> Bool {
        guard let stored = config.config.jobs.first(where: { $0.id == jobID }) else { return true }
        return !stored.matches(job)
    }

    private func save() {
        guard let draft else { return }
        var updated = config.config
        guard let index = updated.jobs.firstIndex(where: { $0.id == draft.id }) else { return }
        updated.jobs[index] = draft
        isSaving = true
        Task {
            let saved = await appState.saveFreeDLConfig(updated)
            isSaving = false
            saveError = saved ? nil : "freedl.yaml was not written. The backend rejected the values above."
            if saved { self.draft = nil }
        }
    }
}

private extension FreeDLJob {
    /// `FreeDLJob` is a wire model, so it stays free of protocol-irrelevant
    /// conformances; the form needs only this comparison.
    func matches(_ other: FreeDLJob) -> Bool {
        id == other.id
            && enabled == other.enabled
            && sourceURL == other.sourceURL
            && libraryDir == other.libraryDir
            && bufferDir == other.bufferDir
            && backupDir == other.backupDir
            && logDir == other.logDir
            && stateFile == other.stateFile
            && planLimit == other.planLimit
            && downloadOrder == other.downloadOrder
            && targetFormat == other.targetFormat
            && minMatchScore == other.minMatchScore
            && ambiguityGap == other.ambiguityGap
            && replaceLimit == other.replaceLimit
            && applyPromotions == other.applyPromotions
    }
}
