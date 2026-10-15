import Foundation

/// Track durations reach the app as whatever `duration of t as text` produced
/// in Music.app's AppleScript: seconds as a real, formatted in the *system*
/// locale, so the same track arrives as `266.029` here and `266,029` on a
/// machine with a comma decimal separator. Rendering that raw under a column
/// headed "Time" is not a time.
///
/// Both `internal/rekordbox/music` and `internal/playlists` read the same
/// AppleScript field, so both surfaces share this one parser rather than each
/// growing its own.
enum MusicDuration {
    /// m:ss, or the raw string when it cannot be parsed — never a value udl
    /// did not report.
    static func label(_ raw: String?) -> String {
        guard let raw, !raw.isEmpty else { return "—" }
        let normalized = raw.replacingOccurrences(of: ",", with: ".")
        guard let seconds = Double(normalized), seconds >= 0, seconds.isFinite else { return raw }
        let whole = Int(seconds.rounded())
        return String(format: "%d:%02d", whole / 60, whole % 60)
    }
}
