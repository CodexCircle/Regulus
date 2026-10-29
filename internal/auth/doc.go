// Package auth implements the hub's local accounts and everything that
// protects the HTTP API: first-run setup of the admin, password hashing,
// login sessions, users, roles and permissions, two-factor authentication
// (TOTP and recovery codes), credential reset and the HTTP middleware.
//
// Reading order:
//
//   - service.go: Service, the entry point that the rest of the hub uses.
//   - users.go, roles.go, permissions.go: accounts and what each role may do.
//   - twofactor.go, totp.go: the second factor.
//   - reset.go: administrator-issued reset links.
//   - secret.go: the key (secret.key in the data directory) that protects
//     the TOTP secrets stored in the database.
//   - store*.go: the database access, split by topic. Nothing outside the
//     package touches these tables.
//   - *_http.go, http.go: the routes and the middleware.
//
// The authentication of agents is not here: it is certificate-based and lives
// in packages pki and fleet.
package auth
