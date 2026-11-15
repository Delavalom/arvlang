# Phase 8.3 notes: independent mutation checks

This file holds the independent mutation-check rounds 1-4 for the "Phase 8.3" section of `16-implementation-notes.md`. They moved here when Phase 3 and Phase 8.3 were merged, because the shared file would otherwise pass the pre-commit hook's size limit. The earlier mutations are in `16b-phase83-notes.md`. Like `16`, this file overrides older plan text.

## Mutation check, round 1 (independent)

Each mutation was applied alone, `go test -count=1 ./...` (unit and e2e) was run, and the file was restored with `git checkout`. The earlier mutations in 16b were not repeated. Two found **test gaps** (the code is correct; nothing caught the mutant); both are now closed by tests that fail under the mutant (checked by reapplying it). Four survivors are equivalent: another check refuses the same input.

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| fs: no swap escape | `openIn` always opens directly (`openLeaf`, no `os.Root`) | yes | `TestReadFileDirSwapCannotEscape`, `TestNestedGrantDirSwapCannotEscape`, `TestMixedKind*`. Not caught by e2e `TestP83SwapRaceCannotEscape` this time (probabilistic) |
| fs: no symlink escape | `openLeaf` drops `O_NOFOLLOW` | yes | `TestReadFileFileGrantLeafSwapIsRefused`, `TestWriteFileLeafSwapCannotEscape` |
| fs: no swap escape | `LineageCaps` returns only the task's own grants | yes | `TestAttenuatedChildSwapCannotEscape`, `TestGrandchildSwapCannotEscape`, `TestLineageCaps` |
| fs: no swap escape | `confiningRoot` returns the innermost matching ancestor | gap, now closed | Was uncaught (a probe with `fs_write=/g`, `fs_write=/g/sub`, `fs_read=/g/sub/deep` and `sub` swapped for a link read `SECRET`). Now caught by `TestConfiningRootPicksOutermostOfSeveral` and `TestThreeLevelNestedGrantSwapCannotEscape` (`threelevel_unix_test.go`). |
| fs: no swap escape | `step` drops `!fi.IsDir()` | no (see round 2) | `step`'s `os.SameFile` check still refuses a symlink, because `Lstat` saw the link's inode. A file fails `OpenRoot` with ENOTDIR. |
| fs: no symlink escape | `openLeafIn` drops its symlink case | no (equivalent) | `os.Root` refuses an escaping link, and an inside one fails `os.SameFile`. The only difference is that the target gets opened before the refusal. |
| fs: no symlink escape | `Canonical` leaves a final symlink unresolved | yes | e2e `TestFSRead` (symlink pointing outside), `TestFSWriteDanglingSymlink`; `TestCanonicalDanglingLeafSymlink`, `FuzzCanonical` |
| I3 (fs) | `fsRequest` opens the path as written, not the canonical one | yes | `TestReadFileIsConfinedToItsGrant`; e2e `TestFSRead`, `TestP83FSReadsUnchanged` |
| net: redirects | `CheckRedirect` parses but no longer checks | yes | `TestHTTPGetChecksRedirectTargets`; e2e `TestNet`, `TestNetPorts`, `TestP83RedirectHops` |
| net: hosts | `http_get` skips its name check (dial check kept) | yes, e2e only | e2e `TestNetHostForms`. In unit tests the dial check refuses these requests anyway, with a different message. |
| net: ports | `netCovers` ignores the grant's port | yes | `TestNetPortCoverage`, `TestAttenuateKeepsPorts`, `TestHTTPGetChecksRedirectPorts`; e2e `TestNetPorts`, `TestP83SpawnNetAttenuation` |
| net: ports | `http` default port is 81 | yes | `TestNetRequestUsesTheSchemeDefaultPort`; e2e `TestP83RedirectHops` |
| net: ports, I3 | `netRequest` drops the port | yes | `TestHTTPGetChecksThePort` and 5 more; e2e `TestNet`, `TestNetPorts` |
| dial: SSRF/rebinding | `Control` returns nil without calling `RequireDial` | yes | `TestHTTPGetRefusesANameThatResolvesToLoopback`; e2e `TestNetDialChecksResolvedAddress`, `TestP83AlternateLoopbackSpellings` |
| dial: SSRF | dial check ignores the port | yes | `TestAllowsDial`, `TestRequireDialNamesTheGrantItNeeds`; e2e `TestNetPorts`, `TestP83UnspecifiedAddressNeedsLoopbackGrant` |
| dial: SSRF | `AddrClass` drops private; 6to4 not decoded; CGNAT `/10` becomes `/11` | yes, unit only | `TestAddrClass`. The hermetic e2e suite reaches only loopback. |
| dial: SSRF, I2 | `*` covers internal IPs other than loopback | yes | `TestAllowsDial`, `TestNetCoverageCIDRAndInternal` |
| scope parsing | `@` allowed; port 0; port 65536 (17-bit parse) | yes | `TestNewRejectsBadNetScopes`, `TestNetScopePorts`; e2e `TestNetGrantValidation`, `TestP83NetValues` |
| scope parsing | CIDR with bits set past the prefix; IPv6 zone; `*.10.0.0.1`; `host..` | yes | `TestNetScopeCIDR`, `TestNetScopeMappedAndZoned`, `FuzzNetScope` seeds; e2e `TestP83NetValues`, `TestNetCIDRAndWildcard`, `TestP83RedirectHops` |
| I2 | a CIDR grant covers a range one bit wider (`q.Bits() >= p.Bits()-1`) | gap, now closed | Was uncaught (a child of `net=10.0.0.0/8` could spawn with `net("10.0.0.0/7")`, gaining `11.0.0.0/8`). Now caught by `TestAttenuateCIDRNeverWidens`, `TestNetCoverageCIDRAndInternal` and `TestNetAttenuationNeverGrantsMore` (vocabulary now has `10.0.0.0/7` and probes `11.1.2.3`). |
| I2 | `*.x.com` covers `x.com` and `evilx.com` | yes, unit only | `TestCovers`, `TestNetCoverage` |
| I2 | `*` covers CIDR ranges | yes | `TestNetAttenuationNeverGrantsMore`, `TestAttenuationNeverGrantsMoreRandom`; e2e `TestP83SpawnNetAttenuation` |
| I2 | `Attenuate` builds with `New`, skipping the coverage check | yes | `TestAttenuate`, `FuzzAdmit`; e2e `TestAttenuation`, `TestP83SpawnNetAttenuation` |
| I1 | `Literal` canonicalises fs scopes (an `lstat` when a value is built) | yes | `TestCapabilityConstructorsDoNotTouchFS`, `TestLiteralDoesNotResolve`; e2e `TestRobustness` |
| leak rule | keep-alives on, `CloseIdleConnections` kept; or the reverse | no | See rounds 2-4. Changing both is caught (16b). |

## Mutation check, round 2 (round 1's survivors)

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| fs: no swap escape | `confiningRoot` returns the innermost matching ancestor | yes, unit only | `TestConfiningRootPicksOutermostOfSeveral`, `TestThreeLevelNestedGrantSwapCannotEscape` |
| fs: no swap escape | `step` drops `!fi.IsDir()` | **no: test gap** | `OpenRoot` on a swapped-in FIFO blocked in `openat` (darwin). Closed in round 3. |
| fs, leak rule | the three survivors rechecked in round 3 | no (judged equivalent) | See rounds 3, 4 |
| I2 | a CIDR grant covers a range one bit wider | yes, unit only | `TestAttenuateCIDRNeverWidens`, `TestNetCoverageCIDRAndInternal`, `TestNetAttenuationNeverGrantsMore` |

## Mutation check, round 3 (round 2's survivors)

Each alone; `go test -count=1 ./...`, `-race` for the leak rule.

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| fs: no swap escape | `step` drops `!fi.IsDir()` | yes, unit only | `TestNestedGrantStepRefusesSwappedFIFO`: both cases hung until the test opened the FIFO at 5 s, then got `not a directory`, not `path changed` |
| fs, leak rule | `openLeafIn` symlink case dropped; keep-alives on; `CloseIdleConnections` removed | no | Rechecked in round 4 |

## Mutation check, round 4 (round 3's survivors)

Each alone; `go test -count=1 ./...`, `-race` for the leak rule.

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| fs: no symlink escape | `openLeafIn` drops its symlink case | yes, unit only | `TestNestedFileGrantLeafSymlinkIsNotFollowed`: 5 subtests got the target's error, not `path changed` |
| leak rule | keep-alives on, `CloseIdleConnections` kept | no (equivalent) | Also `-count=5` on net/leak tests; why in 16b |
| leak rule | `CloseIdleConnections` removed, keep-alives off | yes, unit only | `TestHTTPGetCancelledMidDialLeavesNoDial`: dial open 8 s after cancel |
