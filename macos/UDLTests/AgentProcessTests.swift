import XCTest
@testable import UDL

@MainActor
final class AgentProcessTests: XCTestCase {
    func testInitialStateIsStopped() {
        let process = AgentProcess()
        XCTAssertEqual(process.state, .stopped)
        XCTAssertTrue(process.stderrLog.isEmpty)
    }

    func testStopWithoutLaunchIsIdempotent() async {
        let process = AgentProcess()
        await process.stop()
        await process.stop()
        XCTAssertEqual(process.state, .stopped)
    }

    func testLaunchHandshakeGracefulStopAndRestart() async throws {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "udl-agent-test-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let executable = directory.appending(path: "fixture-agent")
        let script = """
        #!/bin/sh
        while IFS= read -r line; do
          case "$line" in
            *session.initialize*)
              printf '%s\\n' '{"jsonrpc":"2.0","id":"c-1","result":{"protocol_version":1,"build":{"version":"fixture","commit":"test","date":"now"},"methods":[],"working_dir":"/tmp","config_paths":[],"feature_config_paths":{},"capabilities":{}}}'
              ;;
            *session.shutdown*)
              printf '%s\\n' '{"jsonrpc":"2.0","id":"c-2","result":{"shutdown":true}}'
              exit 0
              ;;
          esac
        done
        """
        try script.write(to: executable, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: executable.path)
        setenv("UDL_DEVELOPMENT_BINARY", executable.path, 1)
        defer { unsetenv("UDL_DEVELOPMENT_BINARY") }

        let process = AgentProcess()
        let client = try await process.launch(workingDirectory: directory)
        let initialized = try await client.initialize()
        XCTAssertEqual(initialized.build.version, "fixture")
        XCTAssertEqual(process.state, .running)
        await process.stop()
        XCTAssertEqual(process.state, .stopped)
    }

    func testUnexpectedExitIsRecoverableState() async throws {
        let directory = FileManager.default.temporaryDirectory
            .appending(path: "udl-agent-crash-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let executable = directory.appending(path: "fixture-crash")
        try "#!/bin/sh\nexit 17\n".write(to: executable, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: executable.path)
        setenv("UDL_DEVELOPMENT_BINARY", executable.path, 1)
        defer { unsetenv("UDL_DEVELOPMENT_BINARY") }

        let process = AgentProcess()
        _ = try await process.launch(workingDirectory: directory)
        let deadline = ContinuousClock.now + .seconds(2)
        while process.state == .running && ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(20))
        }
        XCTAssertEqual(process.state, .exited(17))
    }
}
