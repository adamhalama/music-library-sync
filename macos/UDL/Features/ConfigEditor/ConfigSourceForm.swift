import AppKit
import SwiftUI

/// The grouped form for `defaults`, following the System Settings layout the
/// mockup asks for.
struct ConfigDefaultsForm: View {
    @Binding var defaults: MainConfigDefaults

    var body: some View {
        Form {
            Section("Global defaults") {
                LabeledContent("State directory") {
                    HStack(spacing: 8) {
                        TextField("", text: $defaults.stateDir)
                            .font(Typography.monoBody)
                            .labelsHidden()
                        Button("Choose…") { chooseDirectory(into: $defaults.stateDir) }
                    }
                }
                Text("Where per-source state files live. udl creates it on save if it is missing.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)

                LabeledContent("Archive file") {
                    TextField("", text: $defaults.archiveFile)
                        .font(Typography.monoBody)
                        .labelsHidden()
                }

                Stepper(value: $defaults.threads, in: 1...128) {
                    LabeledContent("Threads", value: "\(defaults.threads)")
                }
                Text("1 keeps scdl and deemix stable; higher values are untested.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)

                Toggle("Continue on error", isOn: $defaults.continueOnError)
                Text("Keep going when one source fails. This field is a plain bool in the file, so it has no unset state.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)

                Stepper(value: $defaults.commandTimeoutSeconds, in: 1...86_400, step: 30) {
                    LabeledContent("Command timeout", value: "\(defaults.commandTimeoutSeconds)s")
                }
                Text("Seconds allowed per external command before udl gives up on it.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }
        }
        .formStyle(.grouped)
    }
}

/// The grouped form for one source, including the tri-state policy pickers.
struct ConfigSourceForm: View {
    @Binding var source: MainConfigSource
    let canMoveUp: Bool
    let canMoveDown: Bool
    let onMoveUp: () -> Void
    let onMoveDown: () -> Void
    let onDuplicate: () -> Void
    let onDelete: () -> Void

    var body: some View {
        Form {
            Section("Identity") {
                LabeledContent("ID") {
                    Text(source.id.isEmpty ? "—" : source.id)
                        .font(Typography.monoBody)
                        .textSelection(.enabled)
                }
                ConstraintNote(
                    text: "The id names this source's state file and is what --source matches, so it is set when the source is created and not renamed here. Duplicate the source to make one under a different id."
                )

                Picker("Type", selection: $source.type) {
                    Text("SoundCloud").tag("soundcloud")
                    Text("Spotify").tag("spotify")
                }
                .pickerStyle(.segmented)

                Toggle("Enabled", isOn: $source.enabled)
                Text("Disabled sources are skipped by udl sync unless named explicitly.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }

            Section("Location") {
                LabeledContent("Source URL") {
                    TextField("", text: $source.url)
                        .font(Typography.monoBody)
                        .labelsHidden()
                }
                LabeledContent("Target directory") {
                    HStack(spacing: 8) {
                        TextField("", text: $source.targetDir)
                            .font(Typography.monoBody)
                            .labelsHidden()
                        Button("Choose…") { chooseDirectory(into: $source.targetDir) }
                    }
                }
                LabeledContent("State file") {
                    TextField("", text: Binding(
                        get: { source.stateFile ?? "" },
                        set: { source.stateFile = $0.trimmed.isEmpty ? nil : $0 }
                    ))
                    .font(Typography.monoBody)
                    .labelsHidden()
                }
                Text("A relative name resolves inside defaults.state_dir. Leaving it empty omits the key.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }

            Section("Sync policy") {
                triState(
                    "Break on existing",
                    value: $source.sync.breakOnExisting,
                    hint: "Stop paging once a known track appears."
                )
                triState(
                    "Ask on existing",
                    value: $source.sync.askOnExisting,
                    hint: "Prompt before overwriting a file that is already there."
                )
                triState(
                    "Local index cache",
                    value: $source.sync.localIndexCache,
                    hint: "Faster planning, slightly staler view of the folder."
                )
                ConstraintNote(
                    text: "These three are *bool in the file: unset is a real value that leaves the decision to udl's own default. A plain switch would write an explicit false the first time you saved."
                )
            }

            Section("Adapter") {
                LabeledContent("Kind") {
                    TextField("", text: $source.adapter.kind)
                        .font(Typography.monoBody)
                        .labelsHidden()
                }
                Text(source.type == "spotify"
                     ? "deemix needs a Deezer ARL; spotdl needs Spotify app credentials."
                     : "scdl and yt-dlp must be on PATH.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)

                LabeledContent("Extra arguments") {
                    TextField("", text: Binding(
                        get: { (source.adapter.extraArgs ?? []).joined(separator: "\n") },
                        set: { text in
                            let args = text
                                .split(separator: "\n", omittingEmptySubsequences: true)
                                .map { String($0).trimmed }
                                .filter { !$0.isEmpty }
                            source.adapter.extraArgs = args.isEmpty ? nil : args
                        }
                    ), axis: .vertical)
                    .font(Typography.monoBody)
                    .labelsHidden()
                    .lineLimit(3...8)
                }
                Text("One flag per line, passed to the adapter verbatim.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }

            Section("Source actions") {
                HStack(spacing: 8) {
                    Button("Duplicate", action: onDuplicate)
                    Button("Move up", action: onMoveUp)
                        .disabled(!canMoveUp)
                        .help(canMoveUp ? "Runs this source one place earlier." : "Already first.")
                    Button("Move down", action: onMoveDown)
                        .disabled(!canMoveDown)
                        .help(canMoveDown ? "Runs this source one place later." : "Already last.")
                    Spacer()
                    Button("Delete…", role: .destructive, action: onDelete)
                }
                if !canMoveUp || !canMoveDown {
                    ConstraintNote(
                        text: canMoveUp
                            ? "This source already runs last."
                            : (canMoveDown ? "This source already runs first." : "It is the only source.")
                    )
                }
                Text("Order is the order udl runs sources in. Nothing here is written until you save.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }
        }
        .formStyle(.grouped)
    }

    private func triState(_ title: String, value: Binding<Bool?>, hint: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Picker(title, selection: Binding(
                get: { TriStateBool(value.wrappedValue) },
                set: { value.wrappedValue = $0.value }
            )) {
                ForEach(TriStateBool.allCases) { state in
                    Text(state.label).tag(state)
                }
            }
            Text(hint)
                .font(Typography.caption)
                .foregroundStyle(Theme.textTertiary)
        }
    }
}

/// A real `NSOpenPanel`, as the mockup's "Choose…" implies.
@MainActor
func chooseDirectory(into binding: Binding<String>) {
    let panel = NSOpenPanel()
    panel.canChooseDirectories = true
    panel.canChooseFiles = false
    panel.allowsMultipleSelection = false
    let expanded = (binding.wrappedValue as NSString).expandingTildeInPath
    if !expanded.isEmpty {
        panel.directoryURL = URL(fileURLWithPath: expanded)
    }
    if panel.runModal() == .OK, let url = panel.url {
        binding.wrappedValue = url.path
    }
}
