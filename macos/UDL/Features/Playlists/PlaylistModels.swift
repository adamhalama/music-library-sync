import Foundation

struct PlaylistDefinition: Codable, Sendable, Identifiable {
    let id: String
    var name: String
    var provider: String
    var providerPlaylist: String?
    var providerPlaylistID: String?
    var defaultFreeDLJob: String?
    var defaultRekordboxTarget: String?

    enum CodingKeys: String, CodingKey {
        case id, name, provider
        case providerPlaylist = "provider_playlist"
        case providerPlaylistID = "provider_playlist_id"
        case defaultFreeDLJob = "default_freedl_job"
        case defaultRekordboxTarget = "default_rekordbox_target"
    }
}

struct PlaylistTrack: Codable, Sendable, Identifiable {
    var id: String { "\(index):\(providerID ?? ""):\(databaseID ?? "")" }
    let index: Int
    let providerID: String?
    let databaseID: String?
    let artist: String?
    let title: String
    let album: String?
    let duration: String?
    let path: String?
    let missingLocal: Bool?

    enum CodingKeys: String, CodingKey {
        case index
        case providerID = "provider_id"
        case databaseID = "database_id"
        case artist, title, album, duration, path
        case missingLocal = "missing_local"
    }
}

struct PlaylistSnapshot: Codable, Sendable {
    let version: Int
    let playlistID: String
    let name: String
    let provider: String
    let providerPlaylist: String
    let providerPlaylistID: String?
    let refreshedAt: Date
    @DefaultEmpty var tracks: [PlaylistTrack]
    let checksumSHA256: String

    enum CodingKeys: String, CodingKey {
        case version
        case playlistID = "playlist_id"
        case name, provider
        case providerPlaylist = "provider_playlist"
        case providerPlaylistID = "provider_playlist_id"
        case refreshedAt = "refreshed_at"
        case tracks
        case checksumSHA256 = "checksum_sha256"
    }
}

struct PlaylistListRow: Codable, Sendable, Identifiable {
    var id: String { definition.id }
    let definition: PlaylistDefinition
    let snapshot: PlaylistSnapshot?
    let snapshotError: String?

    enum CodingKeys: String, CodingKey {
        case definition, snapshot
        case snapshotError = "snapshot_error"
    }
}

struct PlaylistListResult: Codable, Sendable {
    @DefaultEmpty var playlists: [PlaylistListRow]
}

struct PlaylistShowResult: Codable, Sendable {
    let definition: PlaylistDefinition
    let snapshot: PlaylistSnapshot
}

struct PlaylistConfig: Codable, Sendable {
    let version: Int
    // `playlists.config.read` returns null here for anyone who has never
    // defined a playlist, which is every fresh install.
    @DefaultEmpty var playlists: [PlaylistDefinition]
}

struct PlaylistConfigResult: Codable, Sendable {
    let path: String
    let config: PlaylistConfig
    let content: String
}

struct PlaylistIDParams: Codable, Sendable {
    let playlistID: String
    enum CodingKeys: String, CodingKey { case playlistID = "playlist_id" }
}

struct ProviderPlaylistListParams: Codable, Sendable {
    let provider: String
}

struct PlaylistDefinitionParams: Codable, Sendable {
    let definition: PlaylistDefinition
}

struct PlaylistDefinitionSaveResult: Codable, Sendable {
    let path: String
    let definition: PlaylistDefinition
    let created: Bool
}

struct PlaylistConfigWriteParams: Codable, Sendable {
    let config: PlaylistConfig
}

struct ProviderPlaylist: Codable, Sendable, Identifiable {
    let name: String
    let providerID: String?
    var id: String { providerID ?? name }
    let smart: Bool?
    let trackCount: Int
    enum CodingKeys: String, CodingKey {
        case name, smart
        case providerID = "id"
        case trackCount = "track_count"
    }
}

struct ProviderPlaylistListResult: Codable, Sendable {
    @DefaultEmpty var playlists: [ProviderPlaylist]
}

struct PlaylistChanges: Codable, Sendable {
    let added: Int
    let removed: Int
    let kept: Int
}

struct PlaylistRefreshResult: Codable, Sendable {
    let snapshot: PlaylistSnapshot
    let changes: PlaylistChanges
    let path: String
}

enum PlaylistOperation: Sendable {
    case providerList
    case refresh(String)
}
