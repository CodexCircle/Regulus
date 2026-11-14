# Remote agents

Besides the built-in agent, a server can run `regulus agent` and be managed from the hub. The agent always connects **out** to the hub (gRPC over TLS 1.3), so the managed server needs no open port.

1. **Turn on the listener.** Remote agents are off by default. In the dashboard, **Servers** says so and someone with `servers.manage` turns them on with a port (8443 by default): the hub starts listening at once, keeps it after a restart and records it in the audit log (`GET`/`PUT /api/agents/listener`). Alternatively set `agent_listen: ":8443"` (or `--agent-listen`, `REGULUS_AGENT_LISTEN`) and restart the hub; the configuration always wins and cannot be changed from the dashboard. Open that port in the firewall only towards the servers you manage.
2. **Make a token.** An admin opens **Servers → Add a server** in the dashboard, optionally names the server and gets the command to run, with a one-time token valid for one hour, the port and the **CA fingerprint** of the hub (the same thing `POST /api/enrollment-tokens` returns). The token is shown once.
3. **Enroll the agent** on the other server, once. On Ubuntu or Debian the easiest way is the installer command shown by the dashboard (see [Installation](installation.md)); by hand, with the binary already there:
   ```sh
   regulus agent --hub HUB_HOST:8443 --token TOKEN --ca-fingerprint sha256:... --data-dir /var/lib/regulus-agent
   ```
   The agent first checks that the hub's certificate is signed by a CA with that fingerprint (so the token is never sent to an impostor), creates its own key, and the hub signs its certificate. The key stays on the agent. From then on the agent only needs `--hub` and the saved credentials; the same options can go in a YAML file (`--config`: `hub`, `data_dir`, `log`) or in `REGULUS_AGENT_*` variables.
4. **Revoke** an agent from its server page or with `DELETE /api/agents/{id}`: its connection is closed and it can never connect again; the process stops with an error. To use that server again, delete its data directory and enroll with a new token.

How it is protected: the hub creates its own certificate authority at the first start (`pki/` in the data directory, `ca.key` readable by the owner only). Tokens are random, stored only as a hash, single use, expire after an hour and are rate limited (10 wrong attempts per 15 minutes per address). Every connection needs the client certificate of an enrolled, not revoked agent, and the agent id comes from the certificate, not from what the agent says. Client certificates last five years; there is no renewal yet. Creating tokens, renaming and revoking agents need `servers.manage` and go to the audit log.

**Versions.** An agent and the hub agree on a protocol version when the agent connects (the agent sends the range it speaks, the hub picks the highest both understand). On top of that the hub accepts agents of **its own release series and the previous one**: the same minor version, one minor behind (hub 0.3.x takes 0.3.x and 0.2.x), or any release of the previous major. Updating the hub first is therefore safe: its agents keep working until they are updated too. The **Servers** page marks agents that are behind the hub, or ahead of it (update the hub). An agent further behind is refused with a message that says so and the server is listed as `incompatible` with the reason. A refusal because of versions does not stop the agent service: it asks again every few seconds, so it connects by itself once the agent or the hub is updated. A revoked agent still stops for good. Development builds are never judged.

## Limits on what an agent can send

An agent is a remote machine, and one that is compromised or buggy must not be able to exhaust the hub. The hub (`internal/hub/limits.go`) enforces:

| Limit | Value | What happens beyond it |
|---|---|---|
| Size of one message on the agent port | 256 KiB | the stream is refused (`ResourceExhausted`) |
| Streams per connection | 8 | the extra stream is refused |
| Texts (host name, versions, mount points, interface names…) | 256 bytes, no control characters | cut, cleaned |
| Disks and network interfaces per report | 64 each | the rest is cut (logged once per stream) |
| Reports | one per half of the metrics interval | the extra reports are dropped (logged once per stream) |

With one report per interval and one hour of history, a single agent keeps at most about 50 KB per report times the number of reports of that hour (for example about 36 MB at the 10 s default in the worst case, a normal host uses a few hundred bytes per report). The hub does not yet keep a global memory budget across agents; the dashboard port also has header and idle limits (`internal/hub/hub.go`).

## When a disk or the collector does not answer

A network or storage mount whose server is gone can block `statfs` for as long as it stays that way, and no signal ends the call. The agent therefore asks every mount at the same time, waits up to **2 seconds** in total, and sends the report **without the mounts that did not answer** (one warning in the log per mount); a mount that is still stuck is not asked again (a new call per report would only pile up stuck threads) and comes back in the reports as soon as it answers. The whole collection also runs beside the loop that sends the heartbeats: while one is still running the next is skipped, so a collector that never returns costs reports, never the connection (the hub drops an agent that stops sending heartbeats). To try it on a real machine see the `[Test]` issue for hung mounts.
