import Foundation

/// What the sidebar is pointing at. `Defaults` is a real object in the file,
/// not a header, so it selects like a source does.
enum ConfigSelection: Hashable {
    case defaults
    case source(String)

    var id: String {
        switch self {
        case .defaults: "<defaults>"
        case .source(let id): "src:\(id)"
        }
    }

    init?(id: String) {
        if id == "<defaults>" {
            self = .defaults
        } else if id.hasPrefix("src:") {
            self = .source(String(id.dropFirst(4)))
        } else {
            return nil
        }
    }
}

/// The two panes the toolbar switches between.
enum ConfigViewMode: String, CaseIterable, Identifiable {
    case form
    case yaml

    var id: String { rawValue }
    var label: String {
        switch self {
        case .form: "Form"
        case .yaml: "YAML"
        }
    }
}

/// A `*bool` policy field. The wire distinguishes "absent" from `false`, so a
/// `Toggle` would silently turn every unset field into an explicit `false` on
/// the next save. Three states are what the file actually has.
enum TriStateBool: String, CaseIterable, Identifiable {
    case unset
    case yes
    case no

    var id: String { rawValue }

    var label: String {
        switch self {
        case .unset: "Default"
        case .yes: "Yes"
        case .no: "No"
        }
    }

    init(_ value: Bool?) {
        switch value {
        case .none: self = .unset
        case .some(true): self = .yes
        case .some(false): self = .no
        }
    }

    var value: Bool? {
        switch self {
        case .unset: nil
        case .yes: true
        case .no: false
        }
    }
}

/// One validation problem, from either side of the wire.
struct ConfigIssue: Identifiable, Hashable {
    enum Origin: Hashable {
        /// Found in the app before `config.validate` is called, so Save can be
        /// blocked without a round trip.
        case local
        /// Reported by `config.validate` or `config.writeFile`.
        case backend
    }

    var id: String { "\(origin):\(sourceID ?? ""):\(message)" }
    let message: String
    var origin: Origin = .local
    /// The source this points at, when it can be attributed, so the sidebar can
    /// mark the offending row instead of marking all of them.
    var sourceID: String?
}

/// Checks the app can make on its own, mirroring what `config.Validate`
/// rejects. These exist to block Save and to attribute a problem to a row
/// before a save is attempted — the backend remains the authority, and every
/// save still calls `config.validate` first.
enum ConfigValidation {
    static func issues(in config: MainConfig) -> [ConfigIssue] {
        var issues: [ConfigIssue] = []

        if config.defaults.stateDir.trimmed.isEmpty {
            issues.append(ConfigIssue(message: "defaults.state_dir is empty."))
        }
        if config.defaults.archiveFile.trimmed.isEmpty {
            issues.append(ConfigIssue(message: "defaults.archive_file is empty."))
        }
        if config.defaults.threads < 1 {
            issues.append(ConfigIssue(message: "defaults.threads must be at least 1."))
        }
        if config.defaults.commandTimeoutSeconds < 1 {
            issues.append(ConfigIssue(message: "defaults.command_timeout_seconds must be at least 1."))
        }
        if config.sources.isEmpty {
            issues.append(ConfigIssue(message: "No sources are defined; udl sync would have nothing to run."))
        }

        var seen: Set<String> = []
        for source in config.sources {
            let id = source.id.trimmed
            if id.isEmpty {
                issues.append(ConfigIssue(message: "A source has an empty id.", sourceID: source.id))
            } else if !seen.insert(id).inserted {
                issues.append(ConfigIssue(message: "Duplicate source id \"\(id)\".", sourceID: source.id))
            }
            if source.url.trimmed.isEmpty {
                issues.append(ConfigIssue(message: "\(label(source)) has no url.", sourceID: source.id))
            }
            if source.targetDir.trimmed.isEmpty {
                issues.append(ConfigIssue(message: "\(label(source)) has no target_dir.", sourceID: source.id))
            }
            if source.adapter.kind.trimmed.isEmpty {
                issues.append(ConfigIssue(message: "\(label(source)) has no adapter.kind.", sourceID: source.id))
            }
            // `config.Validate` requires a state file for both source types it
            // supports, so a draft without one can only ever be refused.
            if (source.stateFile ?? "").trimmed.isEmpty {
                issues.append(ConfigIssue(
                    message: "\(label(source)) has no state_file; udl requires one for soundcloud and spotify alike.",
                    sourceID: source.id
                ))
            }
        }
        return issues
    }

    private static func label(_ source: MainConfigSource) -> String {
        source.id.trimmed.isEmpty ? "An unnamed source" : "Source \"\(source.id)\""
    }
}

extension MainConfigSource {
    /// The duplicate `[` / `D` in the TUI. A copy has to take a new ID or the
    /// file would carry two sources udl cannot tell apart.
    func duplicated(existingIDs: [String]) -> MainConfigSource {
        var candidate = "\(id)-copy"
        var counter = 2
        while existingIDs.contains(candidate) {
            candidate = "\(id)-copy-\(counter)"
            counter += 1
        }
        return MainConfigSource(
            id: candidate,
            type: type,
            enabled: enabled,
            targetDir: targetDir,
            url: url,
            stateFile: stateFile,
            sync: sync,
            adapter: adapter
        )
    }
}
