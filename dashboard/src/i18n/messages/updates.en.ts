// Texts of the Updates page (UpdatesPage).
// Keys start with "updates.". Add the Italian text in updates.it.ts.
export const updates = {
  "updates.title": "Updates",
  "updates.loading": "Loading…",
  "updates.unknown": "unknown",

  "updates.channel.stable": "stable",
  "updates.channel.beta": "beta",
  "updates.channel.alpha": "alpha",

  "updates.version.title": "Version",
  "updates.version.runs": "This hub runs",
  "updates.version.platform": "({platform}).",
  "updates.version.devBuild":
    "This is a development build. It does not compare itself with releases, so it is never offered an update.",
  "updates.version.checksOffBefore":
    "Update checks are turned off in the configuration (",
  "updates.version.checksOffAfter":
    "), so the hub does not contact any server.",
  "updates.version.availableLabel": "Update available",
  "updates.version.availableVersion": "Version {version}",
  "updates.version.isAvailable": "is available.",
  "updates.version.releasePage": "Release page",
  "updates.version.upToDate":
    "Regulus is up to date: the newest {channel} release is {version}.",
  "updates.version.noRelease": "The release server lists no {channel} release.",
  "updates.version.checked": "Checked {ago}.",
  "updates.version.notChecked": "Not checked yet.",
  "updates.version.checkFailed": "The last check failed: {error}",

  "updates.staged.label": "Downloaded update",
  "updates.staged.version": "Version",
  "updates.staged.downloaded": "is downloaded ({size}, {archive}).",
  "updates.staged.signature":
    "Its signature was checked: it was made by this project's release workflow for this version, and the file matches it.",
  "updates.staged.checksum": "Its SHA-256 matches the release's checksum list.",
  "updates.staged.itIs": "It is",
  "updates.staged.notInstalled": "not installed",
  "updates.staged.applyOn": ".",
  "updates.staged.applyOff":
    ": this hub was not set up to install updates itself (updates.apply, and the regulus-update.path unit of the installer).",
  "updates.staged.checksumNote":
    "The checksum list comes from the same place as the file, so it catches a damaged download but does not prove who published it.",

  "updates.downloading": "Downloading…",
  "updates.applying.head": "Installing {what}…",
  "updates.applying.theUpdate": "the update",
  "updates.applying.rest":
    "The hub restarts in a moment and this page may stop answering for a short while; it checks again by itself. If the new version does not start properly, the previous one is put back.",
  "updates.stalled":
    "Nothing has picked up the request for a few minutes. The unit that installs updates is probably not running:",
  "updates.last.applied": "Updated to {version} {ago}.",
  "updates.last.appliedFrom": "Updated to {version} from {from} {ago}.",
  "updates.last.rolledBack":
    "Version {version} did not start properly, so the previous version was put back.",
  "updates.last.notInstalled": "Version {version} was not installed.",
  "updates.downloadFailed": "The download failed: {error}",

  "updates.action.checkNow": "Check now",
  "updates.action.checking": "Checking…",
  "updates.action.download": "Download {version}",
  "updates.action.install": "Install {version}",
  "updates.action.installNow": "Install now",
  "updates.action.cancel": "Cancel",
  "updates.action.save": "Save",
  "updates.action.stop": "Stop",
  "updates.confirm.label": "Confirm installation",
  "updates.confirm.text":
    "The hub is checked and replaced by version {version} and restarts. Agents reconnect by themselves. If the new version does not start, the previous one is put back.",

  "updates.settings.title": "Settings",
  "updates.settings.when": "When a new version is out",
  "updates.mode.manual": "Only tell me",
  "updates.mode.download": "Download and check it, then wait for me",
  "updates.mode.automatic":
    "Download it and install it by itself, inside a maintenance window",
  "updates.settings.applyOff":
    "Installing is not set up on this hub (updates.apply and the regulus-update.path unit, which the installer enables).",
  "updates.settings.windowStarts": "Window starts",
  "updates.settings.windowEnds": "Window ends",
  "updates.settings.windowNote":
    "Times are those of the hub's server. The hub restarts during the update, and a version that had to be rolled back is not tried again by itself.",
  "updates.settings.channel": "Channel",
  "updates.settings.channelStable": "Stable",
  "updates.settings.channelBeta": "Beta (releases and betas)",
  "updates.settings.channelAlpha": "Alpha (every release, alphas included)",

  "updates.rollout.title": "Servers",
  "updates.rollout.intro":
    "The servers this hub manages are updated to the version the hub runs (",
  "updates.rollout.introAfter":
    "). They fetch it from the hub, so they need no internet access, and a server whose new version does not reconnect goes back to the previous one by itself.",
  "updates.rollout.running":
    "Updating the servers, {size} at a time. It stops at the first failure.",
  "updates.rollout.progress": "Update progress",
  "updates.rollout.allCurrent":
    "Every connected server runs {target}, or is not managed by an agent that can be updated.",
  "updates.rollout.behindOne": "1 server runs an older version.",
  "updates.rollout.behindMany": "{count} servers run an older version.",
  "updates.rollout.behindLabel": "Servers behind",
  "updates.rollout.runs": "{name} runs {version}",
  "updates.rollout.atATime": "At a time",
  "updates.rollout.all": "All",
  "updates.rollout.updateOne": "Update this server to {target}",
  "updates.rollout.updateMany": "Update {count} servers to {target}",
  "updates.rollout.lastDone": "Last update of the servers: done.",
  "updates.rollout.lastDoneAgo": "Last update of the servers: done, {ago}.",
  "updates.rollout.lastStopped": "Last update of the servers: stopped.",
  "updates.rollout.lastStoppedAgo":
    "Last update of the servers: stopped, {ago}.",
  "updates.rollout.lastFailed":
    "Last update of the servers: stopped by a failure.",
  "updates.rollout.lastFailedAgo":
    "Last update of the servers: stopped by a failure, {ago}.",
  "updates.rollout.lastLabel": "Last update",

  "updates.agent.pending": "waiting",
  "updates.agent.offered": "fetching the update",
  "updates.agent.installing": "installing, waiting for it to come back",
  "updates.agent.updated": "updated",
  "updates.agent.failed": "failed",
  "updates.agent.skipped": "not tried",
  "updates.agent.row": "{name} ({from}): {status}",
  "updates.agent.rowMessage": "{name} ({from}): {status} — {message}",
} as const;
