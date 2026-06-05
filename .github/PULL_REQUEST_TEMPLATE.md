# Pull Request template for kopiaprofile
#
# We use Conventional Commits (https://www.conventionalcommits.org/)
# for the title. The title (NOT the squash-merge commit message) is
# used in the GitHub release notes; CHANGELOG.md is hand-maintained
# by the maintainer at release time (see docs/release-process.md).

## What does this PR do?

A short summary of the change.

## Why is this change needed?

Link to the issue it closes, or describe the motivation.

## How was it tested?

- [ ] `make build`
- [ ] `make test`
- [ ] `make integration` (if it touches the kopia wrapper, lock,
  secrets, or any code that talks to a real kopia process)
- [ ] Added/updated unit tests for the new behaviour
- [ ] Added/updated integration-test steps if applicable

## Checklist

- [ ] My code follows the project's style guidelines (`make fmt`)
- [ ] I have added an entry under `## [Unreleased]` in `CHANGELOG.md`
      describing the user-visible change (the maintainer curates
      the final section when cutting a release; the in-PR entry
      helps reviewers and keeps the diff focused).
- [ ] My commit messages follow Conventional Commits
      (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `build:`, …)
- [ ] I have considered backwards compatibility and added a note in
      the PR description if breaking

## Conventional Commit type

Pick one (delete the rest):

- [ ] `feat:` — new feature
- [ ] `fix:` — bug fix
- [ ] `refactor:` — neither feature nor fix
- [ ] `perf:` — performance
- [ ] `docs:` — docs only
- [ ] `test:` — tests only
- [ ] `build:` — build system / CI
- [ ] `chore:` — tooling / housekeeping
