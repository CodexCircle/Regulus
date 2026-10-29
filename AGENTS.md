# Notes for AI coding agents

Everything an agent needs to work on Regulus is in this repository. Read these before starting:

- [README.md](README.md): what Regulus does and how each part works.
- [CONTRIBUTING.md](CONTRIBUTING.md): setup, commands, branches, commits, language.
- [docs/architecture.md](docs/architecture.md): how hub, agents, trust, data and updates fit together, with diagrams.
- [docs/glossary.md](docs/glossary.md): what each term means today; add new terms here.
- [docs/decisions.md](docs/decisions.md): why things are the way they are, what is still open, what has not been verified on a real machine.
- [docs/plugins.md](docs/plugins.md): the plugin design draft (milestone M6).

If you learn something that matters for future work and it is not written in one of these files, add it to the right one in the same pull request. Do not rely on context outside the repository.

## How to work

- **One pull request per issue**, referenced with `Closes #N`. Follow CONTRIBUTING.md for branches, Conventional Commits and language (English only for code, comments, commits, issues, pull requests and documents; only the GUI is bilingual, English and Italian).
- **Checks:** run `make lint test` (and `make proto-check` after touching `proto/`) before pushing. CI runs on every pull request and must be green; do not add `[skip ci]` to commits.
- **Package comments:** every package under `internal/` and `cmd/` has a comment that says what it does and what it does not. `golangci-lint` fails when one is missing; keeping it true when you change the package is your job (CI cannot check that).
- **Decisions:** when a pull request makes or changes a design decision, update `docs/decisions.md` in the same pull request. If code and a document disagree, say so and fix the one that is wrong, or open an issue if it needs a decision.
- **New areas start with a design issue.** For a feature that is not yet designed (for example inventory or AI Support), write the design in an issue first and wait for agreement before writing code.
- **Do not overstate verification.** Say what was tested and how. Parts listed in #81 have not run on a real system yet; do not describe them as verified.

## Reporting back

At the end of a piece of work, report:

1. a **completeness score from 0 to 100** for the area you worked on, with the list of what is missing to reach 100;
2. an **issue for each gap** you found (in English, with a "Done when" line), unless one already exists;
3. the **milestone** you chose for each new issue and why (see the table in `docs/decisions.md`).
