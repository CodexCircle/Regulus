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
- **Language**: everything is in English: code, comments, commit messages, issues, pull requests and documents. The only bilingual part is the **GUI** (dashboard and console), which must support English and Italian in every version, alpha included. A pull request that adds or changes a user-facing string provides both the English and the Italian text, and the Italian text is not optional. **Dashboard:** texts live in catalogs, one file per area in `dashboard/src/i18n/messages/` (`<area>.en.ts` is the source: a key like `servers.addTitle` that starts with its area, never the English text, with `{name}` placeholders; `<area>.it.ts` is typed on the English keys, so a missing Italian text does not compile; a test also catches duplicate keys across areas, extra keys, empty texts and different placeholders). Areas: core, auth, servers, updates, users, account. In a component use `const t = useT()` and `t("nav.servers")`. The language is the saved choice (the switch in the header) or the browser's. Migration of the existing strings is in progress, one page group per pull request (#125): a string still written in a component is a known gap, not a pattern to copy; **new strings go in the catalogs**. **Console:** the texts are in `internal/console/strings.go`, one table per language (`en`, `it`); `UseLocale` picks the language from `LC_ALL`, `LC_MESSAGES` or `LANG` (the first one set; `it`, `it_IT.UTF-8`... is Italian). A test fails when a text is empty in either language or a `%s` differs. Health-check names and a few small literals are still written in the code (#191).
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
