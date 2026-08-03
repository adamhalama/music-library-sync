import SwiftUI

/// `.glyph.sc/.sp/.am/.rb` — keyed off `SourceCapability.sourceType`, never off
/// a display string.
struct SourceGlyph: View {
    let sourceType: String
    var size: CGFloat = 17

    private var kind: Kind { Kind(sourceType: sourceType) }

    var body: some View {
        RoundedRectangle(cornerRadius: size * 0.26)
            .fill(
                LinearGradient(
                    colors: kind.colors,
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                )
            )
            .frame(width: size, height: size)
            .overlay(
                Text(kind.abbreviation)
                    .font(.system(size: size * 0.5, weight: .black))
                    .foregroundStyle(.white)
            )
            .accessibilityLabel(kind.name)
    }

    enum Kind {
        case soundCloud
        case spotify
        case appleMusic
        case rekordbox
        case other

        init(sourceType: String) {
            switch sourceType.lowercased() {
            case let value where value.contains("soundcloud"): self = .soundCloud
            case let value where value.contains("spotify"): self = .spotify
            case let value where value.contains("apple") || value.contains("music"): self = .appleMusic
            case let value where value.contains("rekordbox"): self = .rekordbox
            default: self = .other
            }
        }

        var abbreviation: String {
            switch self {
            case .soundCloud: "SC"
            case .spotify: "SP"
            case .appleMusic: "AM"
            case .rekordbox: "RB"
            case .other: "··"
            }
        }

        var name: String {
            switch self {
            case .soundCloud: "SoundCloud"
            case .spotify: "Spotify"
            case .appleMusic: "Apple Music"
            case .rekordbox: "Rekordbox"
            case .other: "Source"
            }
        }

        var colors: [Color] {
            switch self {
            case .soundCloud: Theme.glyphSoundCloud
            case .spotify: Theme.glyphSpotify
            case .appleMusic: Theme.glyphAppleMusic
            case .rekordbox: Theme.glyphRekordbox
            case .other: [Theme.idle, Theme.idle]
            }
        }
    }
}
