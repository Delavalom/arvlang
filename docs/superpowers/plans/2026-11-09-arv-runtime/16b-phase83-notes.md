# Phase 8.3 notes: details, review history and mutations

This file extends the "Phase 8.3" section of `16-implementation-notes.md`, which keeps the summary, the changed expectations and the known limits. The detail lives here because the parallel phases all append to `16-implementation-notes.md`, and the pre-commit hook blocks files over about 20 KB. Like `16`, this file overrides older plan text.

## Filesystem: `os.Root` in detail

- `fsRequest` returns the canonical path and the *outermost* grant of the request's kind that covers it (`CapSet.Grant`, shortest scope; a directory grant wins over a file grant inside it). `openIn` opens that grant as an `os.Root` and opens the path beneath it. `os.Root` follows a symlink only while it stays inside the root, and refuses one that escapes ("path escapes from parent"). So a directory swapped for a symlink after the check can no longer redirect the open outside the grant.
- **Why outermost (review round 1).** `os.OpenRoot` follows symlinks in the root's own path, and `openLeaf` in every directory component. With `fs_read=/g` and `fs_read=/g/sub`, the first build rooted the open at `/g/sub`, which a writer inside `/g` could swap for a symlink to anywhere. `TestNestedGrantDirSwapCannotEscape` covers it.
- **Other kinds and other spellings (review round 2).** `Grant` compares strings of one kind, so `fs_write=/g` with `fs_read=/g/sub`, or `fs_read=/R/g` with `fs_read=/R/G/sub` on case-insensitive APFS, still rooted the open at the inner directory, and the same swap escaped. Now `confiningRoot` picks the outermost ancestor of the covering grant that `os.SameFile` matches with *any* fs grant the task's lineage holds, so case, Unicode-normalisation and bind-mount aliases match too. `openIn` opens an `os.Root` there and steps down to the grant one component at a time (`step`): each must `Lstat` as a directory and open as that same file. A file grant's leaf is opened the same way, without `O_CREATE` if it exists and with `O_EXCL` if not, so a planted symlink is never followed. Tests: `arv/evaluator/mixedswap_unix_test.go` (the `beforeStep` hook swaps between the check of a component and its open).
- A file grant (the path is the grant itself) with no directory grant around it has no directory inside the grant to confine. It keeps the old direct open with `O_NOFOLLOW`.
- **Spawned tasks and missing leaves (review round 3).** `confiningRoot` saw only the task's own grants. A child attenuated to `fs_read("/g/sub")` under a parent holding only `fs_read=/g` rooted its open at `/g/sub`, and a writer inside `/g` could swap `sub` for a symlink out of every grant: the child reached files its parent was refused. `fsRequest` now confines by `Task.LineageCaps`, the grants of the task and every ancestor (only to choose where the open starts; checks use the task's own). Tests: `attenswap_unix_test.go`. Also, `openLeafIn` opened a *missing* leaf for reading through `os.Root`, which follows a symlink planted after the `Lstat`; such a read now fails with ENOENT and opens nothing.
- During such a race, a swap to a *relative* symlink that stays inside the grant is followed. `os.Root` refuses every *absolute* symlink, even one that points inside the root.
- **Unreadable directories (review round 4).** `os.Root` opens each directory it walks, the root included, with `openat(O_RDONLY|O_DIRECTORY)` (`os/root_unix.go`, `rootOpenDir`), which needs the read bit. A plain open needed only the search bit. So a directory without `r` on the way from the open's root to the file now fails with `permission denied`: a `0711` home directory seen by another user, a `0100` directory inside a grant, or a `0300` drop-box grant directory. The failure is closed (it denies, never widens). Falling back to a plain open would reopen the swap race, so it is a known limit, not fixed; a fix needs a hand-written `openat` walk that opens directories for search only (`O_SEARCH` on darwin, `O_PATH` on Linux) and re-implements `os.Root`'s symlink rules. Pinned by `arv/evaluator/unreadabledir_unix_test.go`; checked on `feat/phases-3-8`, where the same test reads "ok" and writes nil. Workaround: grant the readable directory below the unreadable one.

## Network: details

- **Ports.** A port is 1–65535; leading zeros are dropped. Every request names a port: the URL's own port, or the scheme's default (`http` 80, `https` 443). `http_get` and `CheckRedirect` both build the request with `netRequest`. A portless request stands for every port, so at `spawn` only a portless grant covers it (I2).
- **IP literal forms.** IP literals are normalised to the form the dial check compares: an IPv4-mapped address (`::ffff:127.0.0.1`) becomes plain IPv4, in grants and requests alike, and a mapped CIDR range of /96 or longer becomes the IPv4 range it holds (`::ffff:10.0.0.0/104` is `10.0.0.0/8`). Before, such a grant matched a request written the same way but could never pass the dial, which unmaps the address. An IPv6 zone (`fe80::1%en0`) is rejected in a grant or a request URL: the dial check ignores zones, so a zoned grant could never match, and dropping the zone would widen it to every interface.
- **CIDR.** A range with bits set past its prefix is rejected, and the error suggests the masked form. A host *name* never matches a CIDR grant, even when it resolves into the range: requests are checked by name; CIDR and IP grants decide which internal addresses a dial may reach.
- **Dial check.** `transportFor` gives each `http_get` its own `http.Transport`. Its `net.Dialer.Control` runs once per address actually tried, after DNS resolution, and calls `Task.RequireDial`. `runtime.AddrClass` defines internal: unspecified, loopback, private and ULA, link-local (which holds 169.254.169.254), multicast, 0/8, 100.64/10, 192.0.0/24, 198.18/15, 240/4, local NAT64, Teredo and site-local. IPv4-mapped, NAT64 and 6to4 addresses are classed by the IPv4 address they carry. The check runs on the connected address, so there is no resolve-then-check step for a rebinding answer to race.
- `*` covers every host name but no internal IP literal, and no CIDR range. Otherwise a child could attenuate `*` into `net("127.0.0.1")` and gain an internal dial its parent lacked (I2). `TestNetAttenuationNeverGrantsMore` checks this exhaustively over a vocabulary of scope shapes.
- The transport is never shared: a connection checked against one task's grants must not serve another. Keep-alives are off, so no goroutine outlives the call (`TestHTTPGetLeavesNoGoroutines`). No proxy is used: through a proxy the dial would reach the proxy, and the check would see the proxy's address.
- **Bug found by fuzzing.** `FuzzNetScope` found that `net("..")` normalised to `"."`, and `"host.."` to `"host."`. Neither form is accepted on a second pass. Normalisation now rejects a name that still ends in a dot after one trailing dot is dropped.

## Deviations from `15-phase8-tooling.md` §8.3

1. **Acceptance test uses `localhost`, not a `rebind.test` DNS stub.** A stub would need a hand-written DNS codec and a resolver hook in production code. `localhost` resolves to loopback through `/etc/hosts`, which is hermetic, and the check runs on the connected address, so a stub would exercise the same path.
2. **`net.Dialer.Control` instead of resolving inside a custom `DialContext`.** Control sees every address the dialer actually tries, including Happy Eyeballs fallbacks, so there is nothing to keep consistent between a lookup and the connect.
3. **`*` does not cover internal IP literals or CIDR ranges** (see above).
4. **CIDR grants take no port.**
5. **An internal dial needs an IP or CIDR grant covering that address and port**, not just any IP or CIDR grant (§8.3: "unless the grant itself is an IP or CIDR"). `net=127.0.0.1:8080` does not let a granted name dial `127.0.0.1:9090`. Stricter, so safer: a grant for one internal service must not open every other.

## Mutations checked

| Mutation | Caught by |
|---|---|
| `read_file` and `write_file` open the canonical path directly again (no `os.Root`) | `TestReadFileDirSwapCannotEscape`, `TestWriteFileDirSwapCannotEscape`, `TestReadFileLeafSwapCannotEscape` |
| `Control` always returns nil | `TestHTTPGetRefusesANameThatResolvesToLoopback`, `TestHTTPGetRedirectDialIsChecked` |
| Keep-alives on, with no `CloseIdleConnections` | `TestHTTPGetLeavesNoGoroutines` |
| `*` covers internal IP literals | `TestNetAttenuationNeverGrantsMore`, `TestNetCoverageCIDRAndInternal` |
| `Grant` picks the longest covering scope again (review round 1) | `TestNestedGrantDirSwapCannotEscape` (all four cases), `TestGrantPicksTheOutermostCover` |
| IP literals keep the `::ffff:` form | `TestNetScopeMappedAndZoned`, `TestMappedGrantPassesTheDial` |
| `confiningRoot` returns the covering grant (round 2) | `TestMixedKindNestedGrantSwapCannotEscape`, `TestCaseVariantNestedGrantSwapCannotEscape`, `TestMixedKindSwapBetweenCheckAndStep` |
| `confiningRoot` compares scopes as strings | `TestConfiningRootMatchesByIdentity` (a symlink-spelled grant: any filesystem). `TestCaseVariantNestedGrantSwapCannotEscape` skips on case-sensitive ones |
| `fsRequest` confines by the task's own grants (round 3) | `TestAttenuatedChildSwapCannotEscape` (all six cases) |
| `LineageCaps` stops at the parent | `TestGrandchildSwapCannotEscape` |
| `openLeafIn` opens a missing leaf for reading (round 3) | `TestMixedKindMissingLeafReadPlantedSymlink` |
| `step` or `openLeafIn` drops its `os.SameFile` check | `TestMixedKindSwapBetweenCheckAndStep` (dir cases; file cases) |
| A missing leaf is opened without `O_EXCL` | `TestMixedKindMissingLeafPlantedSymlink` |
| Unreadable-directory behaviour changes (round 4; a fix, not a bug, would flip it) | `TestSearchOnlyDirInsideGrantIsRefused`, `TestWriteOnlyGrantDirIsRefused` |
