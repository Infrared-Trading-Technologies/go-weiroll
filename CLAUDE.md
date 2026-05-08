# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Unit tests (no chain). Single test: -run TestName, or sub-test: -run TestName/sub_name
go test ./...
go test -run TestTupleValue -v
go vet ./... && go build ./...

# Run an example (each has its own main package)
go run ./examples/04_tuple_returns/

# Integration tests — separate module under integration/, requires Foundry (forge + anvil)
./integration/run_test.sh                                       # fresh anvil, basic tests only
./integration/run_test.sh --fork --rpc "$MAINNET_RPC_URL"       # full fork suite
./integration/run_test.sh --fork --rpc "$RPC" --test TestForkCase7_AdvancedComposition
```

The integration suite is a separate Go module (`integration/go.mod`) so test-only deps (Foundry artifacts, ethclient, contracts compiled by `forge build`) don't leak into the published library.

## Architecture

The library produces calldata for the on-chain weiroll VM (`integration/contracts/VM.sol`, `CommandBuilder.sol`). Every design constraint traces to invariants enforced by that VM.

**Pipeline.** `Contract.Invoke` → `*Call` (immutable; modifiers like `WithValue`, `Static`, `RawReturn` return new instances) → `Planner.Add` produces `*ReturnValue` placeholders → `Planner.Plan` runs visibility analysis, allocates state slots, and encodes commands. `*Value` types are sealed (`isValue()` unexported); implementations are `LiteralValue`, `ReturnValue`, `StateValue`, `SubplanValue`, `TupleValue`. Each occupies one state slot **except** `*TupleValue`, which expands into one slot per leaf field at `stateManager.getSlotsForValue` time. Public `Call.Args()` arity stays 1:1 with `method.Inputs`; the 1→N expansion happens internally.

**State management.** `stateManager` (`state.go`) dedups literals by `hex(data)`, recycles slots after their last consumer (gated by `WithSlotOptimization`), and enforces 127 max slots. Literals never reuse freed slots — they're in the initial state and must not be overwritten by mid-execution writes.

**Command encoding** (`encoder.go`). Standard 32-byte form holds ≤6 args; >6 forces extended 64-byte form where Word 1's input bytes (5..10) are unused (`0xff`-padded) and ALL arg slots live in Word 2. Dynamic slot indices carry `0x80`; consumer arg bytes set it where the source is dynamic.

**Two non-obvious VM invariants.** Both produce silent reverts surfaced as `ExecutionFailed(_, _, "Unknown")`:
1. **DELEGATECALL preserves CALLVALUE.** If `execute()` is called with `msg.value > 0`, every `pure`/`view`/default-mutability target reverts at its dispatcher's nonpayable guard. Use `NewContract` (CALL frame) for pure helpers; reserve `NewLibrary` (DELEGATECALL) for code that must act AS the VM, and mark such targets `external payable` with an `address(this) != _SELF` direct-call guard (see `integration/contracts/MintAdapter.sol`).
2. **Extended commands put ALL args in Word 2.** Word 1's input slot bytes are unused. Splitting args between the two words silently drops the first six. The encoder pads Word 1's input region with `0xff`.

**Static-slot 32-byte invariant.** `CommandBuilder.sol:46` rejects any non-dynamic-flagged slot whose data isn't 32 bytes. A fully-static tuple struct (e.g. Uniswap V3 `exactInputSingle`'s 7-field params) packs to N*32 bytes and trips this. Use `weiroll.Tuple(field1, field2, ...)` to expand into per-field slots; `NewLiteral` rejects oversized static literals with `ErrStaticTupleTooLarge` pointing at `Tuple`. v1 `Tuple` leaves are static literals only — `*ReturnValue`/`*StateValue`/`*SubplanValue` and dynamic types are rejected at bind time.

**Examples are documentation.** `examples/01_simple_return` … `examples/07_advanced_composition` are the canonical patterns, mirrored 1:1 by fork tests `TestForkCase{1,3,4,6,7}_*` for on-chain validation. The full taxonomy of return-value extraction (Cases 1–6) lives in `docs/return-values.md`. Read it before designing a new "extract this from on-chain" pattern — most pitfalls are documented there.

## Versioning & release workflow

Authoritative reference: `docs/releasing.md`. Summary:

- **Pre-1.0.** Patch (`v0.0.x`) for fixes / additive API / refactors. Minor (`v0.x.0`) when callers should review their callsites — behavior changes, encoder bytes-changed bugs, strict-mode rejections that previously passed silently. **Never** major before v1.0.0.
- **Tags must be annotated** (`git tag -a vX.Y.Z -m "..."`) and reachable from `main`. Don't tag commits that aren't on `main`. Don't `--force` push to `main`.
- **Don't retract a pushed tag.** Module proxy caches it. Ship `vX.Y.Z+1` and document the bad version in its release notes.
- **Don't bump versions inside files.** The git tag IS the version; `go.mod`'s `module` line names the module, not the version.
- Pre-tag gate: `go test ./...` AND `./integration/run_test.sh --fork --rpc "$MAINNET_RPC_URL"` both green at the tagged commit.

Workflow:
```bash
git push origin main
git tag -a vX.Y.Z -m "vX.Y.Z: <subject>\n\n<bullets>"
git push origin vX.Y.Z
gh release create vX.Y.Z --title "vX.Y.Z — <subject>" --notes "..."
curl -sL "https://proxy.golang.org/github.com/branched-services/go-weiroll/@latest"  # verify
```

Release notes group by impact: **Fixes**, **New API**, **Upgrade notes**. Skip empty sections.

**Commit message style.** `type: subject` lowercase. Established types: `feat`, `fix`, `docs`, `refactor`, `test`. Body explains *why* and the *non-obvious what*; describe the change, not the task.

## Documentation map

- `README.md` — public-facing intro, contract-type rule, command-encoding layout.
- `docs/return-values.md` — six-case taxonomy for extracting on-chain values; read before authoring a recipe.
- `docs/releasing.md` — full release procedure and recovery cheatsheet.
- `integration/README.md` — fork-test inventory and the three funding patterns (pre-funded VM, inline ETH + pure helpers, inline ETH + delegatecall adapter).
