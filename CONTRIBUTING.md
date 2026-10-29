# Contributing to Regulus

Thanks for helping with Regulus. This guide covers how to set up, how we work and what a good pull request looks like.

## Setup

Requirements: Go 1.26+, Node.js 22+, `make`. For linting, `golangci-lint` v2.

```sh
git clone https://github.com/CodexCircle/Regulus.git
cd Regulus
make build        # builds the dashboard, then bin/regulus
make test         # go vet, go test -race, dashboard typecheck and tests
make lint         # golangci-lint and ESLint
make smoke        # starts bin/regulus and checks it end to end
```

See [docs/development.md](docs/development.md) and [docs/configuration.md](docs/configuration.md) for development and configuration, [docs/architecture.md](docs/architecture.md) for the project layout, and [docs/decisions.md](docs/decisions.md) for the reasons behind the main choices and the open questions. AI coding agents: see [AGENTS.md](AGENTS.md).

## How we work

- **One pull request per issue.** Pick an issue (or open one), say you are working on it, and reference it in the pull request with `Closes #N`.
- **Branches** are named after the work, for example `feat/hub-auth` or `fix/migration-rollback`.
- **Commits** follow [Conventional Commits](https://www.conventionalcommits.org/): `feat(hub): ...`, `fix(storage): ...`, `docs: ...`, `ci: ...`, `test: ...`.
- **Run `make lint test` before pushing.** CI runs the same checks and must be green to merge.
- **Protobuf**: after editing a `.proto` file run `make proto` and commit the generated code in `internal/gen`. CI fails if it is out of date.
- **Language**: everything is in English: code, comments, commit messages, issues, pull requests and documents. The only bilingual part is the **GUI** (dashboard and console), which must support English and Italian in every version, alpha included. A pull request that adds or changes a user-facing string provides both the English and the Italian text, and the Italian text is not optional. Today the dashboard has its strings written directly in the components (`dashboard/src`), and the console keeps its English strings in one table (`internal/console/strings.go`) with no Italian version yet; there is no translation mechanism yet (#125).
- **Decisions**: a pull request that makes or changes a design decision updates [docs/decisions.md](docs/decisions.md) too.
- Keep changes focused. Unrelated clean-ups belong in their own pull request.

## Pull request checklist

- [ ] The change is covered by tests, or you explain why it is not.
- [ ] `make lint test` passes locally.
- [ ] Documentation is updated (README, comments, this guide) when behavior changes. If you change what a package does, its package comment (`doc.go` or the file with `// Package ...`) still matches.
- [ ] Every new or changed user-facing GUI string has both an English and an Italian text.
- [ ] The description says what changed and why, and how you tested it.

## Reporting security problems

Do not open a public issue. See [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
