# Phase 10 start: `parse_json` and `http(req)`

Branch `feat/p10-json-http`. Two builtins from 01-phases.md, Phase 10 "Data" and "HTTP".
Code: `arv/evaluator/parsejson.go`, `arv/evaluator/httpreq.go`, the shared request path in
`arv/evaluator/httpget.go` (`doRequest`, `bodyStaysHome`), `arv/check/http.go`.

## Design decisions

**parse_json(text)** (pure, no grant)

- Own recursive-descent parser, not `encoding/json`: it must charge what it builds as it builds it,
  stop on cancellation, read integers exactly and match `json`'s depth limit.
- Objects become hashes with string keys in the text's order. `null` is `nil`.
- Numbers. Arv has integers only (D3 open). A number is accepted when its exact value is an
  integer in the int64 range, however it is written: `1e3` is 1000, `1.0` is 1, `1.50e1` is 15,
  `-9.223372036854775808e18` is MinInt64. `1.5` and `1e-1` are errors that say "Arv has no floats
  yet"; `9223372036854775808` and `1e19` say "out of the integer range". The value is computed
  from the digit string and a saturating exponent, never through a float, so `1e999999999999999999999`
  is cheap and exact.
- Duplicate keys are refused (`parse_json: duplicate key "a" at line 1, column 10`). Go keeps the
  last one; two readers that disagree on a document's value is a known attack shape, and `json`
  never writes a duplicate, so the round trip is unaffected. Two different keys with the same
  64-bit FNV hash (value.Hash's key) are refused too, with their own message (no test: no
  collision is known).
- Invalid UTF-8 and lone surrogates in a string become U+FFFD, as Go reads them and as `json`
  writes them. A BOM is a syntax error, as in Go.
- Errors are `runtime` errors with the line and column (1-based, bytes). Running out of text is
  `unexpected end of JSON input`, as in Go. Quoted text is clipped (`quoteClipped`, `clipped`).
- Budget. Charged before it is built, in chunks of `value.InspectChunk` (64 KiB) as `json` does:
  string bytes (a string is scanned first, so its exact size is charged before it is built), 16
  per array element, 48 per hash pair plus its key's bytes, and 48 per array or hash. On a syntax
  or budget error the pending part of the last chunk is not charged (as in `json`).
- Cancellation: every 64 KiB of text read (white space and string bodies included) and at every
  charge. A small text never looks, which keeps the steps/bytes identical on both engines.
- Depth: a container at depth `value.MaxInspectDepth` (10000) fails, exactly as `json` fails, so
  everything `json` writes reads back and nothing deeper is read.
- Steps: none of its own, like `json`; the call ticks.

**http(req)** (effect `http`, needs `net`)

- `req` keys: `url` (required), `method` (default GET; GET HEAD POST PUT PATCH DELETE OPTIONS,
  exactly, upper case), `headers` (hash of string to string), `body` (string). Any other key is an
  error, so a typo (`header`) or the roadmap's `timeout_ms` is not silently ignored.
- Result: `{status, body}`, the shape of `http_get`. `http_get(url)` now builds the same request
  and goes through `doRequest`: error texts, charges and checks are unchanged (every existing test
  is green). It keeps its own effect name, so old logs replay.
- Validation runs before anything is required, charged or sent; keys are read in the hash's
  order so the first error is stable.
- Headers: names must be RFC 9110 tokens; values may not hold a control byte but tab (so no CR,
  LF, NUL, DEL). Refused: `Host`, `Content-Length` and the hop-by-hop set `Connection`,
  `Keep-Alive`, `Proxy-Connection`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailer`,
  `Transfer-Encoding`, `Upgrade`. A name given twice in any case is refused. At most 64 headers
  and 64 KiB of header lines; a body of at most 10 MiB (the response cap).
- Charges: after the grant check and before sending, the body plus `len(name)+len(value)+4` per
  header. The response is charged as `http_get`'s is.
- Redirects: the existing CheckRedirect (grant with port, URL length, 10 hops) plus
  `bodyStaysHome`: Go resends the body on a 307 or 308 wherever the Location points, so a redirect
  that would carry a body to another `host:port` than the first request's is refused
  (`a 307 redirect to localhost:PORT would send the request body to another host`, kind effect).
  301/302/303 turn the request into a body-less GET (Go). `Authorization` and `Cookie` are dropped
  by Go on a redirect to another domain and kept for the same host; both are tested.
- Record/replay (I7): `Effect: "http"`, so the one gate logs the request hash as the program
  wrote it (method, url, headers, body) and the response. A replay answers from the log; another
  request (header, body or method changed) diverges and the message shows both, clipped to 200
  bytes each by the runtime. Request headers, `Authorization` included, are in the log (0600);
  the README says so. A `secret` type is a later Phase 10 item.
- `arv check`: `http` is a `net` effect and dial-checked. Its host is known when the argument is
  a hash literal, or a name bound once to one, whose keys are all known strings, one of them
  `url` bound to a known string. Two `url` keys, an unknown key, or a non-string url give the `#`
  note `host not statically known`. A literal with known keys and no `url` is
  `never allowed: the request has no url`.
- Redaction (Phase 7): denials are `runtime.DeniedError`s as for `http_get`, so the existing
  text redaction covers them; tested through an agent tool.

## Guarantees and the tests that hold them

| Guarantee | Tests |
| --- | --- |
| `parse_json(json(v))` equals `v` (strings valid UTF-8); otherwise a valid-UTF-8 value that round-trips exactly | `TestParseJSONRoundTripProperty` (3000 random values, the deepest value `json` writes), `FuzzParseJSON` |
| parse_json reads what `encoding/json` reads, and refuses only floats, out-of-range numbers and duplicate keys beyond that | `FuzzParseJSON` (differential against `encoding/json` with `UseNumber`) |
| Numbers: exact integer value or a clear error; no wrap past uint64 | `TestParseJSONValues`, `TestParseJSONErrors` |
| Charged before built, at most one 64 KiB chunk uncharged; exact sizes | `TestParseJSONCharges`, `TestParseJSONStopsAtTheLimit` (heap under 8 MiB past a 1 MiB limit) |
| Cancellable on long text, white space included | `TestParseJSONStopsWhenCancelled` |
| Depth exactly `json`'s; error text bounded | `TestParseJSONNestingLimit`, `TestParseJSONErrorTextIsBounded` |
| I3: `net` grant on host and port before sending; the name is checked, not only the address dialled | `TestHTTPNeedsNetCapability`, `TestHTTPChecksTheNameBeforeTheDial`, e2e `TestP10HTTPDeniedWithoutGrant` |
| I3: dial check, internal address needs an IP grant | `TestHTTPRefusesANameThatResolvesToLoopback`, e2e `TestP10HTTPInternalAddressThroughANameGrant` |
| I3: every redirect re-checked, port included | `TestHTTPRedirectToAnUngrantedHostIsDenied`, e2e `TestP10HTTPRedirectToUngrantedHost` |
| No body to another host on a redirect; Go's method and header rules | `TestHTTPRedirectNeverResendsTheBodyToAnotherHost`, `TestHTTPRedirectBodyRules`, `TestHTTPRedirectDropsSensitiveHeadersForAnotherHost`, e2e `TestP10HTTPRedirectKeepsTheBodyHome` |
| No header smuggling; bounded headers and body | `TestHTTPRequestErrors`, `FuzzHTTPRequestHeaders` (accepted requests write and read back as exactly one header) |
| Request charged before it is sent | `TestHTTPChargesTheRequestBeforeSending` |
| No proxy from the environment; response cap | `TestHTTPUsesNoProxy`, `TestHTTPResponseLimit` |
| I7: logged, replayed offline, divergence names both requests | `TestReplayOfHTTPIsOffline`, `TestReplayOfAChangedHTTPRequestDiverges`, e2e `TestP10HTTPRecordReplay` |
| Classification (pure / effect) in the gate and in `arv check` | `TestEveryBuiltinIsPureOrAnEffect`, `TestEveryBuiltinIsClassified`, `TestHTTPNeeds`, `TestParseJSONIsPure`, e2e `TestP10Check` |
| `arv check` soundness with `http` leaves | `TestReportedGrantsSufficeAndAreNeeded` (a new `http` leaf in the generator) |
| Phase 7 redaction | `TestAgentToolHTTPDenialIsRedacted` |
| Both engines agree (values, errors, positions, steps, bytes) | `TestParseJSONEnginesAgree`, `TestHTTPEnginesAgree`; e2e under `ARV_E2E_TAGS=arv_treewalk` |

## Fuzzing (foreground, darwin/arm64)

| Target | Seconds | Execs | New interesting | Failures |
| --- | --- | --- | --- | --- |
| `FuzzParseJSON` | 120 | 681,185 | 113 | 0 |
| `FuzzHTTPRequestHeaders` | 120 | 1,564,681 | 100 | 0 |

An earlier 20 s run of `FuzzHTTPRequestHeaders` failed on the test's own bound: an invalid header
name of non-printable bytes quotes to about 4 bytes per byte (the existing `quoteClipped`, 256 bytes
in), over the 400 bytes the test first asked for. The bound in both fuzz targets is now 2048 bytes;
the input stays as a seed (`testdata/fuzz/FuzzHTTPRequestHeaders/c1857529aa0cd599`).

## Mutation check

33 hand-applied mutants, one at a time, restored with `git checkout`. 30 were caught at once; M5,
M12 and M25 survived and got tests (now caught). M13 and M25 first did not compile and were
rewritten to compile.

| # | Mutant | Caught by |
| --- | --- | --- |
| M1 | nesting `>=` to `>` | `TestParseJSONNestingLimit` |
| M2 | no duplicate-key check | `TestParseJSONErrors` |
| M3 | negative exponent accepted | `TestParseJSONErrors` |
| M4 | MinInt64 refused | `TestParseJSONValues`, round-trip property |
| M5 | 19-digit bound to 20 (uint64 wraps) | survived; now `TestParseJSONErrors` (`18446744073709551617`) |
| M6 | container uncharged | `TestParseJSONCharges` |
| M7 | array element uncharged | `TestParseJSONCharges` |
| M8 | string uncharged | `TestParseJSONCharges` |
| M9 | no final settle | `TestParseJSONCharges` |
| M10 | never polls for cancellation | `TestParseJSONStopsWhenCancelled` |
| M11 | control bytes allowed in strings | `TestParseJSONErrors` |
| M12 | surrogate pair charged 3, not 4 | survived; now `TestParseJSONCharges` (escape cases) |
| M13 | invalid UTF-8 kept raw | `TestParseJSONValues`, `FuzzParseJSON` seed |
| M14 | trailing data allowed | `TestParseJSONErrors` |
| M15 | object key need not be a string | `TestParseJSONErrors` |
| M16 | `Host` settable | `TestHTTPRequestErrors`, fuzz seed |
| M17 | LF allowed in header values | `TestHTTPRequestErrors` |
| M18 | space allowed in header names | `TestHTTPRequestErrors` |
| M19 | duplicate headers allowed | `TestHTTPRequestErrors` |
| M20 | body follows any redirect | `TestHTTPRedirectNeverResendsTheBodyToAnotherHost` |
| M21 | request not charged | `TestHTTPChargesTheRequestBeforeSending` |
| M22 | header line size without CRLF and `: ` | `TestHTTPRequestErrors`, `TestHTTPChargesTheRequestBeforeSending` |
| M23 | default method POST | `TestHTTPMethods`, `TestHTTPGetIsSugar` |
| M24 | any method allowed | `TestHTTPRequestErrors`, fuzz seed |
| M25 | no grant check before sending (dial check only) | survived; now `TestHTTPChecksTheNameBeforeTheDial` |
| M26 | headers not set on the request | `TestHTTPPostWithHeadersAndBody`, redirect tests |
| M27 | unknown request keys ignored | `TestHTTPRequestErrors` |
| M28 | body cap off by one | `TestHTTPRequestErrors` |
| M29 | header count cap off by one | `TestHTTPRequestErrors` |
| M30 | check: a second `url` key ignored | `TestHTTPNeeds` |
| M31 | check: `http` not dial-checked | `TestHTTPNeeds` |
| M32 | check: no `url` gives "not known" | `TestHTTPNeeds` |
| M33 | check: an unknown key skipped | `TestHTTPNeeds` |

Not tried as non-equivalent: comparing a redirect's host with the previous hop instead of the first
request. Any hop to another host is refused at that hop, so both forms refuse the same chains.

## Known limits

- No response headers in the result: the shape stays `http_get`'s `{status, body}`. A program
  cannot read `Location`, `Content-Type` or a rate-limit header yet.
- `timeout_ms` (in the roadmap) is not a request key yet: it is refused as unknown. The scope's
  `timeout_ms` and the 30 s client timeout bound a request.
- Request headers and body are in the log as written, `Authorization` included (log mode 0600).
  The `secret` type will store a hash instead.
- A divergence message clips each request to 200 bytes (the runtime's rule), so a change far
  into a long body diverges but may not show in the text.
- Go's header rule on redirects is by domain: `Authorization` goes to a subdomain of the first
  host (and to another port on it). That host must still be granted.
- Header values keep leading and trailing spaces; servers usually trim them. Values may hold
  bytes above 0x7F, as HTTP allows.
- Two different JSON keys with the same 64-bit FNV hash are refused with their own message; no
  collision is known, so that branch has no test.
- Error text quotes at most 256 bytes of program text, but a byte that is not printable quotes as
  `\xNN`, so an error can reach about 1 KiB.

## Verification runs (darwin/arm64, Go 1.26.2)

| Command | Result |
| --- | --- |
| `go vet ./...` | clean |
| `go test -race -timeout 40m ./arv/... ./cmd/...` | all 11 packages ok |
| `go test -timeout 30m ./e2e/` | ok (91 s) |
| `go test -tags arv_treewalk -timeout 40m ./...` | all ok but one timing test in `arv/resume_deadline_test.go` (see below) |
| `ARV_E2E_TAGS=arv_treewalk go test -timeout 30m ./e2e/` | ok (101 s) |

`TestResumeWaitForFinishedTasksTakesNoTime` failed twice in the full tree-walker run of `./arv`
(its recording ran past a 4.5 s scope deadline) and passed 3 times out of 3 alone. The base commit
(3f49420) fails the same way in the same run, on a sibling test in that file
(`TestResumeReplaysAFailedAttemptToItsEnd`, 1.5 s deadline). These tests time CPU spin loops
against wall-clock deadlines, so they flake under load. Neither touches `http` or `parse_json`.
