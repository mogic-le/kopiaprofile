# Pull Request template for kopiaprofile
#
# We use Conventional Commits (https://www.conventionalcommits.org/)
# for the title. The PR title (NOT the squash-merge commit message) is
# what `git-chglog` parses to populate CHANGELOG.md and the GitHub
# release notes.

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
- [ ] I have added a newsfragment under `newsfragments/` OR updated
      `CHANGELOG.md` manually (the release workflow will run
      `git-chglog` and overwrite either way, but the manual entry
      helps reviewers).
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
