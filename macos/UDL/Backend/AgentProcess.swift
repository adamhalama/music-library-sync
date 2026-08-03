import Combine
import Darwin
import Foundation

private final class LockedStderrBuffer: @unchecked Sendable {
    private let lock = NSLock()
    private var value = ""
    private let maximumBytes: Int

    init(maximumBytes: Int) { self.maximumBytes = maximumBytes }

    func reset() {
        lock.lock()
        value = ""
        lock.unlock()
    }

    func append(_ chunk: String) {
        let redacted = redactedDiagnostic(chunk)
        lock.lock()
        value.append(redacted)
        if value.utf8.count > maximumBytes {
            value = String(decoding: value.utf8.suffix(maximumBytes), as: UTF8.self)
        }
        lock.unlock()
    }

    func snapshot() -> String {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

@MainActor
final class AgentProcess: ObservableObject {
    enum State: Equatable {
        case stopped
        case launching
        case running
        case failed(String)
        case exited(Int32)

        var label: String {
            switch self {
            case .stopped: "Offline"
            case .launching: "Starting"
            case .running: "Connected"
            case .failed: "Needs attention"
            case .exited: "Exited"
            }
        }
    }

    @Published private(set) var state: State = .stopped
    @Published private(set) var backendPath = ""
    var stderrLog: String { stderrBuffer.snapshot() }

    private(set) var connection: JSONRPCConnection?
    private(set) var client: UDLClient?
    private var process: Process?
    private var stdinPipe: Pipe?
    private var stdoutPipe: Pipe?
    private var stderrPipe: Pipe?
    private var stopping = false
    private let stderrBuffer = LockedStderrBuffer(maximumBytes: 128 * 1024)

    func launch(workingDirectory: URL) async throws -> UDLClient {
        guard process == nil else {
            if let client { return client }
            throw JSONRPCConnectionError.disconnected("backend launch is incomplete")
        }
        state = .launching
        stopping = false
        stderrBuffer.reset()

        let executable = try Self.resolveBackendExecutable()
        backendPath = executable.path
        let environment = try await Self.backendEnvironment()
        let stdinPipe = Pipe()
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        let process = Process()
        process.executableURL = executable
        process.arguments = ["agent", "--working-dir", workingDirectory.path]
        process.currentDirectoryURL = workingDirectory
        process.environment = environment
        process.standardInput = stdinPipe
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        let stderrBuffer = self.stderrBuffer
        stderrPipe.fileHandleForReading.readabilityHandler = { handle in
            let data = handle.availableData
            guard !data.isEmpty, let chunk = String(data: data, encoding: .utf8) else { return }
            stderrBuffer.append(chunk)
        }
        process.terminationHandler = { [weak self] process in
            Task { @MainActor in
                guard let self else { return }
                // `stop()` may already have torn this generation down and
                // launched a replacement. Clearing unconditionally here would
                // then delete the *new* process, so only the generation that
                // is still current may act.
                guard self.process === process else { return }
                self.stderrPipe?.fileHandleForReading.readabilityHandler = nil
                await self.connection?.close(reason: "backend exited with status \(process.terminationStatus)")
                self.teardown()
                if self.stopping {
                    self.state = .stopped
                } else {
                    self.state = .exited(process.terminationStatus)
                }
            }
        }

        do {
            try process.run()
        } catch {
            stderrPipe.fileHandleForReading.readabilityHandler = nil
            state = .failed(error.localizedDescription)
            throw error
        }

        let connection = JSONRPCConnection(
            readHandle: stdoutPipe.fileHandleForReading,
            writeHandle: stdinPipe.fileHandleForWriting
        )
        await connection.start()
        let client = UDLClient(connection: connection)
        self.process = process
        self.stdinPipe = stdinPipe
        self.stdoutPipe = stdoutPipe
        self.stderrPipe = stderrPipe
        self.connection = connection
        self.client = client
        state = .running
        return client
    }

    func restart(workingDirectory: URL) async throws -> UDLClient {
        await stop()
        return try await launch(workingDirectory: workingDirectory)
    }

    func stop() async {
        guard let process else {
            state = .stopped
            return
        }
        stopping = true
        if let client {
            _ = try? await client.shutdown()
        }
        let deadline = ContinuousClock.now + .seconds(2)
        while process.isRunning && ContinuousClock.now < deadline {
            try? await Task.sleep(for: .milliseconds(50))
        }
        if process.isRunning {
            process.terminate()
            try? await Task.sleep(for: .milliseconds(300))
        }
        if process.isRunning {
            kill(process.processIdentifier, SIGKILL)
        }
        await connection?.close(reason: "application shutdown")
        // `terminationHandler` runs on its own queue and hops to the main
        // actor, so it has usually not run yet. `restart()` calls `launch()`
        // straight after this, and `launch()` returns the existing client when
        // `process` is still set — which would hand back the client of the
        // connection just closed, and the next request would fail with
        // "connection is closed". Tearing down here makes the stop synchronous
        // from the caller's point of view.
        teardown()
        state = .stopped
    }

    /// Drops one backend generation. Safe to run twice: the termination
    /// handler and `stop()` race, and whichever arrives second finds nothing.
    private func teardown() {
        stderrPipe?.fileHandleForReading.readabilityHandler = nil
        process = nil
        client = nil
        connection = nil
        stdinPipe = nil
        stdoutPipe = nil
        stderrPipe = nil
    }

    private static func resolveBackendExecutable() throws -> URL {
        let environment = ProcessInfo.processInfo.environment
        var candidates: [URL] = []
        if let development = environment["UDL_DEVELOPMENT_BINARY"], !development.isEmpty {
            candidates.append(URL(fileURLWithPath: development))
        }
        if let embedded = Bundle.main.url(forResource: "udl", withExtension: nil) {
            candidates.append(embedded)
        }
        let cwd = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
        candidates.append(cwd.appending(path: "bin/udl"))
        candidates.append(URL(fileURLWithPath: "/opt/homebrew/bin/udl"))
        candidates.append(URL(fileURLWithPath: "/usr/local/bin/udl"))
        if let executable = candidates.first(where: {
            FileManager.default.isExecutableFile(atPath: $0.path)
        }) {
            return executable
        }
        throw CocoaError(.fileNoSuchFile, userInfo: [
            NSLocalizedDescriptionKey:
                "Could not locate udl. Set UDL_DEVELOPMENT_BINARY for development or embed Contents/Resources/udl."
        ])
    }

    private static func backendEnvironment() async throws -> [String: String] {
        var environment = ProcessInfo.processInfo.environment
        let loginPath = try await Task.detached(priority: .utility) {
            let process = Process()
            let output = Pipe()
            process.executableURL = URL(fileURLWithPath: "/bin/zsh")
            process.arguments = ["-lc", "printf %s \"$PATH\""]
            process.standardOutput = output
            process.standardError = Pipe()
            try process.run()
            process.waitUntilExit()
            guard process.terminationStatus == 0 else {
                throw JSONRPCConnectionError.disconnected("could not resolve login-shell PATH")
            }
            return String(
                decoding: output.fileHandleForReading.readDataToEndOfFile(),
                as: UTF8.self
            )
        }.value
        var entries = loginPath.split(separator: ":").map(String.init)
        for fallback in ["/opt/homebrew/bin", "/usr/local/bin"] where !entries.contains(fallback) {
            entries.append(fallback)
        }
        environment["PATH"] = entries.joined(separator: ":")
        return environment
    }
}
