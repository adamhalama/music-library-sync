import Foundation

/// Which configured sources actually consume a credential.
///
/// `credentials.list` is metadata-only and reports no such field, so the
/// mockup's "Used by" column would have been fabricated. It is derived instead
/// from `sources.capabilities`, mirroring the rules `internal/doctor` uses when
/// it decides which credential checks to emit: a SoundCloud source on the
/// `scdl` family needs the SoundCloud client ID, and a Spotify source on the
/// `deemix` adapter needs both the Deezer ARL and the Spotify application
/// credentials.
///
/// When no configured source uses a credential the column says exactly that,
/// rather than showing a plausible-looking workflow name.
func credentialConsumers(_ kind: CredentialKind, sources: [SourceCapability]) -> [String] {
    sources.filter { credentialApplies(kind, to: $0) }.map(\.sourceID)
}

func credentialApplies(_ kind: CredentialKind, to source: SourceCapability) -> Bool {
    switch kind {
    case .soundCloudClientID:
        source.sourceType == "soundcloud" && source.adapter.hasPrefix("scdl")
    case .deemixARL, .spotifyApp:
        source.sourceType == "spotify" && source.adapter == "deemix"
    }
}

extension CredentialStatus {
    /// C13 — the running value does not come from Keychain.
    ///
    /// `storage_source` is one of `""`, `env`, `keychain`, `spotdl_config`, or
    /// `unknown`, and `external_override` is a *health* value, never a storage
    /// one. Both spellings are checked because the backend sets the health when
    /// it resolves the value and the storage source when it records the check.
    var isExternallyOverridden: Bool {
        health == "external_override"
            || storageSource == "env"
            || storageSource == "spotdl_config"
    }

    /// `health` and `storage_source` arrive as wire tokens; both read as prose.
    var healthLabel: String {
        health.isEmpty ? "unknown" : health.replacingOccurrences(of: "_", with: " ")
    }

    var storageLabel: String {
        switch storageSource {
        case "": "not stored"
        case "keychain": "macOS Keychain"
        case "env": "environment variable"
        case "spotdl_config": "~/.spotdl/config.json"
        default: storageSource.replacingOccurrences(of: "_", with: " ")
        }
    }

    var isMissing: Bool {
        health == "missing" || health == "unavailable"
    }

    /// The one action the row offers. An overridden credential is never offered
    /// "Replace": replacing the Keychain entry would not change the value the
    /// backend actually uses, and the button must not imply otherwise.
    var actionLabel: String {
        if isExternallyOverridden { return "Move to Keychain" }
        return isMissing ? "Add…" : "Replace"
    }

    /// The environment variables `internal/auth` reads before Keychain.
    var environmentVariables: [String] {
        switch kind {
        case .soundCloudClientID: ["SCDL_CLIENT_ID"]
        case .deemixARL: ["UDL_DEEMIX_ARL"]
        case .spotifyApp: ["UDL_SPOTIFY_CLIENT_ID", "UDL_SPOTIFY_CLIENT_SECRET"]
        }
    }
}
