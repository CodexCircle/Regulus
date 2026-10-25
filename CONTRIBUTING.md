# Contributing to Regulus

**English** · [Italiano](#italiano)

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

See the [README](README.md#development) for configuration and the project layout.

## How we work

- **One pull request per issue.** Pick an issue (or open one), say you are working on it, and reference it in the pull request with `Closes #N`.
- **Branches** are named after the work, for example `feat/hub-auth` or `fix/migration-rollback`.
- **Commits** follow [Conventional Commits](https://www.conventionalcommits.org/): `feat(hub): ...`, `fix(storage): ...`, `docs: ...`, `ci: ...`, `test: ...`.
- **Run `make lint test` before pushing.** CI runs the same checks and must be green to merge.
- **Protobuf**: after editing a `.proto` file run `make proto` and commit the generated code in `internal/gen`. CI fails if it is out of date.
- **Language**: code, comments, commit messages and issues are in English. User-facing documents (README, this guide) are in English followed by Italian. Issues are written in English followed by Italian.
- Keep changes focused. Unrelated clean-ups belong in their own pull request.

## Pull request checklist

- [ ] The change is covered by tests, or you explain why it is not.
- [ ] `make lint test` passes locally.
- [ ] Documentation is updated (README, comments, this guide) when behavior changes.
- [ ] The description says what changed and why, and how you tested it.

## Reporting security problems

Do not open a public issue. See [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).

---

# Italiano

Grazie per l'aiuto a Regulus. Questa guida spiega come preparare l'ambiente, come lavoriamo e com'è fatta una buona pull request.

## Preparazione

Requisiti: Go 1.26+, Node.js 22+, `make`. Per il lint serve `golangci-lint` v2. I comandi sono quelli della sezione inglese qui sopra; configurazione e struttura del progetto sono nel [README](README.md#sviluppo).

## Come lavoriamo

- **Una pull request per issue.** Scegli un'issue (o aprine una), scrivi che ci stai lavorando e citala nella pull request con `Closes #N`.
- **Branch** con nomi che descrivono il lavoro, per esempio `feat/hub-auth` o `fix/migration-rollback`.
- **Commit** secondo [Conventional Commits](https://www.conventionalcommits.org/): `feat(hub): ...`, `fix(storage): ...`, `docs: ...`, `ci: ...`, `test: ...`.
- **Esegui `make lint test` prima del push.** La CI esegue gli stessi controlli e deve essere verde per fare il merge.
- **Protobuf**: dopo aver modificato un file `.proto` esegui `make proto` e committa il codice generato in `internal/gen`. La CI fallisce se non è aggiornato.
- **Lingua**: codice, commenti, messaggi di commit e issue sono in inglese. I documenti per gli utenti (README, questa guida) sono in inglese seguito dall'italiano. Le issue sono scritte in inglese seguito dall'italiano.
- Mantieni le modifiche mirate: le pulizie non correlate vanno in una pull request a parte.

## Checklist della pull request

- [ ] La modifica è coperta da test, oppure spieghi perché non lo è.
- [ ] `make lint test` passa in locale.
- [ ] La documentazione è aggiornata (README, commenti, questa guida) se cambia il comportamento.
- [ ] La descrizione spiega cosa è cambiato, perché e come l'hai verificato.

## Segnalare problemi di sicurezza

Non aprire un'issue pubblica. Vedi [SECURITY.md](SECURITY.md).

## Licenza

Contribuendo accetti che i tuoi contributi siano rilasciati con licenza [Apache 2.0](LICENSE).
