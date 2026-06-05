# Release process

kopiaprofile releases are cut by hand-curated commits to `CHANGELOG.md`
plus a `git tag`. The release workflow (`.github/workflows/release.yml`)
does the rest: build with `goreleaser`, then copy the relevant section
of `CHANGELOG.md` into the GitHub release notes.

This document is the maintainer's checklist. The user-facing short
version is in `README.md#release-process`.

## Branch / commit hygiene

1. `main` is the release branch. Do not push tags to topic branches.
2. `CHANGELOG.md` is committed in the same PR that introduces the
   change. The PR template asks for a `## [Unreleased]` entry.
3. We follow [Conventional Commits][cc] for the PR title. The title
   (NOT the squash-merge commit message) is what appears in the
   "What's Changed" list of the GitHub release.

   [cc]: https://www.conventionalcommits.org/

## Cutting a release

```bash
# 1. Make sure main is up to date and the working tree is clean.
git checkout main
git pull --rebase

# 2. Move the "Unreleased" section of CHANGELOG.md into a new
#    "## [<version>] - <YYYY-MM-DD>" block, leave an empty
#    "## [Unreleased]" stub behind, and add a compare-link footer.
$EDITOR CHANGELOG.md
git add CHANGELOG.md
git commit -m "docs: release 0.2.0"

# 3. Tag the release. The tag MUST be the version prefixed with 'v'
#    (e.g. v0.2.0). GoReleaser reads the version from the tag.
git tag -a 0.2.0 -m "feat: release 0.2.0"
git push origin main 0.2.0
```

> **Note:** the tag in the example is `0.2.0` (no `v` prefix) but the
> tag that triggers the workflow is `v0.2.0` (with `v`). The
> `workflow_dispatch` input accepts either; the tag-push trigger only
> matches `v*`. Pick one convention and stick to it. The current
> convention is **tag-with-v-pushed-as-bare-version** — push
> `v0.2.0` literally, not `0.2.0` then.

## What the release workflow does

`.github/workflows/release.yml` runs on every `v*` tag push. It:

1. Checks out the repo with full history.
2. Runs `goreleaser release --clean`, which produces the artefacts
   (tarballs, zips, `.deb`/`.rpm`/`.apk` packages, SHA256SUMS, cosign
   keyless signature, CycloneDX SBOM) and creates a **draft** GitHub
   release with a `goreleaser`-generated body (an auto-list of
   conventional-commit groups).
3. Runs a follow-up `gh release edit` step that replaces those
   notes with the hand-written body of `CHANGELOG.md`'s
   `## [<version>]` section, plus a header line and a footer linking
   to the README's install section.
4. (Optional) Posts a Discord notification if the release fails.

The release starts as a **draft**; review it, fix typos, then click
"Publish".

## If the release notes extraction misses

The awk-based extractor looks for `## [<version>]` (square brackets).
If you tag `v1.0.0-rc1` but the `CHANGELOG.md` section is
`## [1.0.0-rc1] - 2026-06-05`, the version still matches because the
extractor strips the leading `v` from the tag and matches `[<ver>]`.

If the extraction fails, the step emits a `::warning::` annotation
and leaves the goreleaser-generated notes in place — the release is
NOT failed. To fix it: edit the section header to literally match
the tag (without `v`).

## Hot-fix releases

Same as a normal release. Just make sure `CHANGELOG.md`'s
`## [Unreleased]` block gets a properly-tagged new version
(`## [0.2.1]`, `## [0.2.2]`, …) and a fresh date.

## `goreleaser` config

`.goreleaser.yml` declares the matrix of OS/arch combinations, the
archive naming scheme, the `.deb`/`.rpm`/`.apk` package metadata,
cosign signing and the SBOM generator. Its `changelog:` block is
**disabled** — the release workflow is the source of truth for
release notes.

To preview the artefacts locally without publishing:

```bash
make snapshot
# or:
goreleaser release --clean --snapshot --skip=publish,validate
```
