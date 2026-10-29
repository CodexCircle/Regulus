// Package update finds, downloads, verifies and installs new releases of
// Regulus. The hub drives it; the privileged step runs in a separate process.
//
// The pieces, in the order a release goes through them:
//
//   - source.go, fetch.go: ask the release source what is newest on the chosen
//     channel and download files.
//   - signature.go: verify the checksums and the Sigstore signature of a
//     release.
//   - stage.go: keep verified archives in a local cache.
//   - manager.go: Manager, the entry point. It holds the settings (mode:
//     manual, download or automatic; channel; maintenance window), checks for
//     releases, downloads, and asks for the installation.
//   - apply.go, installer.go: the handover to the root helper. The hub writes
//     a request naming a version into a directory it owns; a systemd path unit
//     starts `regulus update-apply`, which checks everything again, installs,
//     and rolls back if the new version does not come up healthy.
//   - version.go, compat.go: version parsing and the rule of which agent
//     versions a hub accepts.
//
// Updating the agents in waves is done by package hub (rollout.go), using the
// release files this package stages.
package update
