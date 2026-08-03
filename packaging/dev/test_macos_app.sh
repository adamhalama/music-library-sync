#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source_dir="${repo_root}/macos/UDL"
test_dir="${repo_root}/macos/UDLTests"
shim_source="${repo_root}/macos/UDLCommandLineTests/XCTestShim.swift"

[[ "$(uname -s)" == "Darwin" ]] || {
  echo "the native command-line test runner requires macOS" >&2
  exit 1
}
command -v xcrun >/dev/null

case "$(uname -m)" in
  arm64) swift_target="arm64-apple-macosx14.0" ;;
  x86_64) swift_target="x86_64-apple-macosx14.0" ;;
  *)
    echo "unsupported development architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/udl-swift-tests.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT
module_cache="${TMPDIR:-/tmp}/udl-swift-test-module-cache"
mkdir -p "$module_cache"

production_sources=()
while IFS= read -r source; do
  production_sources+=("$source")
done < <(find "$source_dir" -type f -name '*.swift' ! -name 'UDLApp.swift' | sort)

test_sources=()
while IFS= read -r source; do
  test_sources+=("$source")
done < <(find "$test_dir" -maxdepth 1 -type f -name '*Tests.swift' | sort)

xcrun swiftc \
  -swift-version 6 \
  -strict-concurrency=complete \
  -parse-as-library \
  -enable-testing \
  -emit-library \
  -emit-module \
  -module-name UDL \
  -module-cache-path "$module_cache" \
  -target "$swift_target" \
  "${production_sources[@]}" \
  -o "${work_dir}/libUDL.dylib"

xcrun swiftc \
  -swift-version 6 \
  -strict-concurrency=complete \
  -parse-as-library \
  -emit-library \
  -emit-module \
  -module-name XCTest \
  -module-cache-path "$module_cache" \
  -target "$swift_target" \
  "$shim_source" \
  -o "${work_dir}/libXCTest.dylib"

runner="${work_dir}/Runner.swift"
{
  echo 'import Darwin'
  echo 'import XCTest'
  echo ''
  echo '@main @MainActor struct CommandLineTestMain {'
  echo '    static func main() async {'
  echo '        var passed = 0'
  echo '        var failed = 0'
  echo '        func run(_ name: String, _ body: () async throws -> Void) async {'
  echo '            let before = XCTFailureRecorder.failureCount'
  echo '            do {'
  echo '                try await body()'
  echo '            } catch {'
  echo '                XCTFailureRecorder.record("threw: \(error)")'
  echo '            }'
  echo '            let failures = XCTFailureRecorder.failures(since: before)'
  echo '            if failures.isEmpty {'
  echo '                passed += 1'
  echo '                print("PASS \(name)")'
  echo '            } else {'
  echo '                failed += 1'
  echo '                print("FAIL \(name)")'
  echo '                for failure in failures { print("  \(failure)") }'
  echo '            }'
  echo '        }'
  for source in "${test_sources[@]}"; do
    while IFS=' ' read -r test_class method is_async is_throwing; do
      call_prefix=""
      if [[ "$is_throwing" == "1" ]]; then call_prefix="try "; fi
      if [[ "$is_async" == "1" ]]; then call_prefix="${call_prefix}await "; fi
      printf '        await run("%s.%s") { %s%s().%s() }\n' "$test_class" "$method" "$call_prefix" "$test_class" "$method"
    done < <(awk '
      /^final class [A-Za-z0-9_]+: XCTestCase/ {
        current = $3
        sub(/:$/, "", current)
      }
      /^[[:space:]]*func test[A-Za-z0-9_]+\(/ {
        method = $0
        sub(/^[[:space:]]*func /, "", method)
        sub(/\(.*/, "", method)
        is_async = ($0 ~ /\)[[:space:]]+async([[:space:]]|\{)/) ? 1 : 0
        is_throwing = ($0 ~ /\)[^{]*throws([[:space:]]|\{)/) ? 1 : 0
        print current, method, is_async, is_throwing
      }
    ' "$source")
  done
  echo '        print("Swift tests: \(passed) passed, \(failed) failed")'
  echo '        if failed > 0 { exit(1) }'
  echo '    }'
  echo '}'
} > "$runner"

xcrun swiftc \
  -swift-version 6 \
  -strict-concurrency=complete \
  -module-cache-path "$module_cache" \
  -target "$swift_target" \
  -I "$work_dir" \
  -L "$work_dir" \
  -lUDL \
  -lXCTest \
  "${test_sources[@]}" \
  "$runner" \
  -Xlinker -rpath \
  -Xlinker "$work_dir" \
  -o "${work_dir}/UDLCommandLineTests"

"${work_dir}/UDLCommandLineTests"
