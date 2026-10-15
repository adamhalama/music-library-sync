import Foundation

enum DoctorPresentationState: Equatable {
    case healthy
    case warning
    case blocked
}

func doctorPresentationState(_ report: DoctorResult) -> DoctorPresentationState {
    if report.checks.contains(where: { $0.status == "blocked" }) {
        return .blocked
    }
    if report.exitCode != 0 || report.checks.contains(where: { $0.status == "warning" }) {
        return .warning
    }
    return .healthy
}

/// Where a failing check leads.
///
/// `DoctorCheck` carries a name, a human `detail`, and an optional
/// `remediation` sentence — but no machine-readable remediation target. The
/// deep link is therefore a keyword match, and it lives here rather than
/// inside a view so Home and Doctor can never route the same check two ways.
/// If this proves brittle the honest fix is a protocol field, which is out of
/// scope for a frontend-only change; until then the fallback is always
/// "Details", never a button that guesses.
enum DoctorFix: Equatable, Sendable {
    case credentials
    case rekordbox
    case config
    /// udl reports nothing actionable for this check.
    case none

    init(_ check: DoctorCheck) {
        let text = "\(check.name) \(check.detail) \(check.remediation ?? "")".lowercased()
        if Severity.forCheck(check) == .ok {
            self = .none
            return
        }
        if text.contains("credential") || text.contains("keychain")
            || text.contains("arl") || text.contains("client id")
            || text.contains("client secret") {
            self = .credentials
        } else if text.contains("rekordbox") {
            self = .rekordbox
        } else if text.contains("config.yaml") || text.contains("configuration")
            || text.contains("config file") {
            self = .config
        } else {
            self = .none
        }
    }

    var destination: AppState.Destination? {
        switch self {
        case .credentials: .credentials
        case .rekordbox: .rekordbox
        case .config: .config
        case .none: nil
        }
    }

    var actionLabel: String? {
        switch self {
        case .credentials: "Open Credentials"
        case .rekordbox: "Open Rekordbox Sync"
        case .config: "Open Advanced Config"
        case .none: nil
        }
    }

    /// Home's compact wording for the same routing decision.
    var shortActionLabel: String {
        switch self {
        case .credentials: "Add credential"
        case .rekordbox: "Rekordbox"
        case .config: "Edit config"
        case .none: "Details"
        }
    }
}

/// Doctor renders one section per severity, worst first.
let doctorSeverityOrder: [Severity] = [.error, .warn, .info, .ok, .idle]

/// `doctor.run` reports a flat list of checks whose `name` is the category
/// ("dependency", "filesystem", "security", …). The sidebar groups by it.
func doctorCategories(_ report: DoctorResult) -> [String] {
    var seen = Set<String>()
    return report.checks.compactMap { seen.insert($0.name).inserted ? $0.name : nil }
}

/// The resolved dependency path `doctor.run` reports for a check, matched by
/// name. Returns nil rather than a plausible-looking guess when nothing
/// matches — a fabricated path is worse than an absent one.
func doctorResolvedPath(for check: DoctorCheck, in report: DoctorResult) -> (name: String, path: String)? {
    let haystack = "\(check.name) \(check.detail)".lowercased()
    let match = report.resolvedDependencies
        .filter { haystack.contains($0.key.lowercased()) }
        .max(by: { $0.key.count < $1.key.count })
    return match.map { (name: $0.key, path: $0.value) }
}
