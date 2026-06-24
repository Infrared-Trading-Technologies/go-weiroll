# Release workflow

How to ship a new version of go-weiroll. Pre-1.0; conventions may tighten after a stable API exists.

## TL;DR

```bash
# 1. Land changes on main (PR or direct push for trivial cases)
git push origin main

# 2. Pick the next version (see "Picking a version" below)
# 3. Tag (annotated) and push
git tag -a vX.Y.Z -m "vX.Y.Z: <one-line summary>

<bullet list of what changed>"
git push origin vX.Y.Z

# 4. Publish a GitHub Release (uses the same notes for human readers)
gh release create vX.Y.Z --title "vX.Y.Z — <subject>" --notes "$(cat <<'EOF'
## Fixes
...
## New API
...
## Upgrade notes
...
EOF
)"

# 5. Verify the Go module proxy picked it up
curl -sL "https://proxy.golang.org/github.com/Infrared-Trading-Technologies/go-weiroll/@latest"
```

That's the entire process. Go modules are published purely via git tags — no registry, no CI step, nothing else.

## What goes into a release

Each tag should be:

- On `main`, at a commit where unit tests pass (`go test ./...`) and fork tests pass (`./integration/run_test.sh --fork --rpc "$MAINNET_RPC_URL"`). Don't tag a broken commit.
- Annotated (`git tag -a`), not lightweight. Annotated tags carry tagger, date, and a message that shows up in `git show vX.Y.Z` and on GitHub.
- Mirrored as a GitHub Release for human-readable notes.

## Picking a version

Format: `vMAJOR.MINOR.PATCH`. Go tooling discovers versions via git tags; the `v` prefix is required.

**While pre-1.0 (`v0.x.y`):**
- Bump **patch** (`v0.0.3 → v0.0.4`) for fixes, additive API, internal refactors. This is the default.
- Bump **minor** (`v0.0.x → v0.1.0`) when you want to signal "review your callsites" — e.g., a behavior change that might surprise someone (a typing fix that changes which type a `*ReturnValue` carries, a fix to an encoder bug that changes the bytes a plan produces).
- Don't bump **major** before `v1.0.0`.

**After `v1.0.0` (full semver):**
- Patch: bug fixes, no API changes.
- Minor: backwards-compatible additions.
- Major: breaking changes (removals, signature changes, behavior changes downstream code can observe).

When in doubt, bump higher. A user expecting patch and getting minor isn't burned; a user expecting patch and getting breaking changes is.

## Commit message conventions

The project uses `type: subject` (lowercase type). Established types in the log:

- `feat` — new functionality, exported API additions.
- `fix` — bug fix.
- `docs` — documentation only.
- `refactor` — code restructure with no behavior change.
- `test` — test-only changes.

Body: explain *why* and the *non-obvious what*. Reference file paths when the change is localized. Commits should describe the change, not the task they came from.

## Release notes structure

Group by impact, not by commit. Three sections work for almost everything:

- **Fixes** — what was broken, what's fixed. Include the failure mode if it was silent, so readers can grep their own logs.
- **New API** — what's new and exported.
- **Upgrade notes** — anything a downstream consumer needs to look at, including pre-1.0 caveats.

Skip sections that are empty. Use backticks for symbols, link to PRs/issues if any.

## What NOT to do

- **Don't `git push --force` to main.** Anyone who fetched the old commit now has divergent history. If you need to undo, push a revert commit.
- **Don't delete or move a tag once pushed.** The Go module proxy caches tags aggressively (often within minutes of publish) and other consumers may already be pinned to it. If a tagged release is broken, ship `vX.Y.Z+1` with the fix and document the bad version in its release notes — don't try to retract.
- **Don't tag a commit that isn't on `main`.** Versions should be reachable from `main`; otherwise downstream consumers can't audit history cleanly.
- **Don't bump the version in code/files.** Go modules don't have a version field — the tag is the version. (`go.mod`'s `module` line names the module; it doesn't track version.)

## Recovery cheatsheet

| Situation | What to do |
|---|---|
| Tagged the wrong commit (not yet pushed) | `git tag -d vX.Y.Z` and re-tag at the right commit. |
| Tagged the wrong commit (already pushed) | Ship `vX.Y.Z+1` with the correct content. Don't retract. |
| Wrong release notes on GitHub | `gh release edit vX.Y.Z --notes "..."` — fine, this is just the human-readable layer. |
| Bad code shipped under a tag | Ship `vX.Y.Z+1` with the fix. Edit the bad release's notes to point at the replacement. |
| `go get @latest` didn't see the new version | Wait a few minutes for the proxy. If still missing: `curl https://proxy.golang.org/github.com/Infrared-Trading-Technologies/go-weiroll/@v/vX.Y.Z.info` to force a fetch. |

## Verification

After the tag is pushed, the Go module proxy should pick it up within minutes:

```bash
curl -sL "https://proxy.golang.org/github.com/Infrared-Trading-Technologies/go-weiroll/@v/list" | sort -V
curl -sL "https://proxy.golang.org/github.com/Infrared-Trading-Technologies/go-weiroll/@latest"
```

The `@latest` response is the source of truth for what `go get @latest` will resolve to.
