# Database

The hub keeps its accounts, sessions, layouts, enrolled agents and update state in one database. Two backends exist: **SQLite**, the default, which needs nothing, and **PostgreSQL**, which you prepare and point the hub at. The administrator chooses between them in the installer ([Installation](installation.md#choosing-the-database), #123); moving existing data from one to the other is #124 (not implemented). The settings below can also be set by hand.

Both backends run the same code: the tests of the storage, auth, fleet, hub, update and command packages run on SQLite and on PostgreSQL in CI. The PostgreSQL job runs on versions 14 and 16 (14 is the minimum, 16 is the newest we test), and a separate CI job connects with `sslmode=verify-full` and `sslrootcert` to a PostgreSQL 16 that has TLS on (see below). What has **not** been checked yet is a PostgreSQL install through the installer on a real VM, including `--db-local` with `apt`, and with a database on another host (#81, #141; the checklist there needs a PostgreSQL item).

## Settings

| Key | Environment | Default | Meaning |
|---|---|---|---|
| `database.type` | `REGULUS_DATABASE_TYPE` | `sqlite` | `sqlite` or `postgres` |
| `database.host` | `REGULUS_DATABASE_HOST` | empty | PostgreSQL host (required for `postgres`) |
| `database.port` | `REGULUS_DATABASE_PORT` | `5432` | PostgreSQL port |
| `database.name` | `REGULUS_DATABASE_NAME` | `regulus` | database name |
| `database.user` | `REGULUS_DATABASE_USER` | `regulus` | role the hub connects as |
| `database.password` | `REGULUS_DATABASE_PASSWORD` | empty | **environment only**, see below |
| `database.sslmode` | `REGULUS_DATABASE_SSLMODE` | `prefer` | `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full` |
| `database.sslrootcert` | `REGULUS_DATABASE_SSLROOTCERT` | empty | CA file for `verify-ca` and `verify-full` |
| `database.schema` | `REGULUS_DATABASE_SCHEMA` | empty (`public`) | schema that holds the tables |

All of them need a restart. They appear in `regulus config list`, in the console and in the dashboard settings.

**The password.** It is read only from the environment, in practice from `/etc/regulus/regulus.env` (mode `0640`, `root:regulus`, loaded by the service unit). It is not a YAML key: the hub refuses a `password:` in `config.yaml` (the file is parsed with unknown keys rejected), `regulus config set database.password` refuses with an explanation, and everywhere a setting is shown (`config get`, `config list`, the console, the dashboard) the value appears as `********` when set. Logs and `regulus db check` never include it, and error messages from the database are rewritten so they do not echo connection strings.

**TLS.** The default `prefer` encrypts when the server offers it and does not check the certificate; it is meant for a PostgreSQL on the same machine or a trusted network. For a database on another host use `verify-full` with `sslrootcert`. CI checks that `verify-full` with the right CA connects over an encrypted session, and that it refuses a host name the certificate does not name, a root that did not issue the certificate and a missing root file, without showing the password (`TestPostgresVerifyFull*`, which need `REGULUS_TEST_TLS_URL` and `REGULUS_TEST_TLS_CA` and are skipped without them). That uses a throwaway CA and a certificate for `localhost` on one machine; it does not cover a real remote host, a certificate chain with an intermediate, or `verify-ca`.

## Preparing PostgreSQL

Supported versions: **14 or newer** (14 is in Ubuntu 22.04, 16 in Ubuntu 24.04). `regulus db check` refuses older ones. The tests run on 14 and 16 (matrix in `.github/workflows/ci.yml`).

Run as a PostgreSQL superuser (for example `sudo -u postgres psql`):

```sql
CREATE ROLE regulus LOGIN PASSWORD 'choose-a-long-random-password';
CREATE DATABASE regulus OWNER regulus;
\c regulus
CREATE EXTENSION IF NOT EXISTS citext;
```

- The role must own the database (or at least be able to create tables in the schema). The migrations create tables, indexes and, once, the `citext` extension (account and role names compare without regard to case, like the `COLLATE NOCASE` columns of SQLite). `citext` is a *trusted* extension from PostgreSQL 13, so the owner can create it, but creating it yourself as above avoids needing that. On Debian and Ubuntu it is in `postgresql-contrib` (installed with the server since PostgreSQL 10 packages).
- The extension is created in schema `public`, and `public` stays on the hub's search path even with a custom `database.schema`.
- A custom schema must exist: `CREATE SCHEMA regulus AUTHORIZATION regulus;`. The hub refuses to start if `database.schema` names a schema it cannot see, instead of quietly using `public`.
- If the database is on another host: `listen_addresses` in `postgresql.conf`, a `hostssl regulus regulus <hub address>/32 scram-sha-256` line in `pg_hba.conf`, and the firewall open on port 5432 only from the hub.

Then check it, as root, from the hub machine (it reads `/etc/regulus/regulus.env` for the password):

```bash
sudo regulus db check
```

It connects with the configured settings and prints the backend, the server version, the schema version and whether the role can create tables and the extension, without changing anything that stays. Exit status `0` is usable, `1` is not (the message says what to fix: wrong password, host unreachable, database missing, missing privilege, schema from a newer Regulus), `2` is a usage error.

## What to back up

With PostgreSQL the data is in the database, but **the key stays on the hub's disk**: the TOTP secrets are encrypted with a key derived from `secret.key` in `data_dir`, and the certificate authority for agents is in `data_dir/pki`. Back up the database **and** `data_dir` together. A database restored without `secret.key` cannot decrypt two-factor secrets.

## Failure behaviour

- At start the hub waits up to 60 seconds for PostgreSQL to answer, then exits with the reason; the service manager starts it again. A database that comes up a little after the hub (after a reboot) needs nobody.
- While running, the connection pool (at most 10 connections, recycled after 30 minutes) opens new connections by itself after PostgreSQL restarts. Requests made during the outage fail with an error; the hub does not exit. Every 15 seconds the hub checks the database, logs **once** when it stops answering and once when it answers again, and reports it in `regulus status` and in the Health section of the console (`check.database`). The dashboard shows it too: a banner above every page, also before sign-in, says when the database stops answering and when it answers again (in English or Italian, by browser language). It comes from `GET /api/database`, which needs **no login** because the hub cannot check a session while the database is down; it carries one boolean (`ok`) and never the reason, which stays in the log, `regulus status` and the console. `/healthz` still answers `ok`, because the installer and the update helper wait for it and a hub that outlives its database is not dead.

## Migrations

The two backends have their own migration files (`internal/storage/migrations/sqlite` and `.../postgres`) with the same numbers and names, because the SQL dialects differ (types, defaults, how a table is rebuilt). A test applies both and compares tables, columns, null-ability, primary and foreign keys (with their actions) and indexes. Rules that hold on both: each migration runs in a transaction, and a hub refuses to start on a database that has migrations it does not know (written by a newer Regulus). On PostgreSQL the whole run holds an advisory lock, so two hubs that start together on one database (for example a redundant hub, #103) migrate it once, one after the other.
