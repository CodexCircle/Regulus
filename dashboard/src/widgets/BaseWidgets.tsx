import { Chart } from "../Chart";
import { formatBytes, formatDuration, formatPercent, formatRate } from "../format";
import type { MetricsPoint } from "../api";
import { AgentView, Waiting } from "./AgentView";
import type { WidgetProps } from "./registry";

const times = (points: MetricsPoint[]) => points.map((p) => Date.parse(p.at));
const percent = (used: number, total: number) => (total > 0 ? (100 * used) / total : 0);

function Stat({ value, caption }: { value: string; caption?: string }) {
  return (
    <p className="stat">
      <strong>{value}</strong>
      {caption && <span>{caption}</span>}
    </p>
  );
}

// SystemWidget shows what the server is and its current load.
export function SystemWidget(props: WidgetProps) {
  return (
    <AgentView {...props}>
      {({ agent, stale, latest }) => {
        const rows: [string, string][] = [
          ["Server", agent.hostname ? `${agent.hostname} (${agent.id})` : agent.id],
          ["System", [agent.os_version ?? agent.os, agent.arch].filter(Boolean).join(" ") || "-"],
          ["Kernel", agent.kernel || "-"],
          ["CPUs", agent.cpu_count ? String(agent.cpu_count) : "-"],
          ["Memory", agent.memory_total_bytes ? formatBytes(agent.memory_total_bytes) : "-"],
          ["Status", stale ? "stale" : agent.status],
          ["Agent version", agent.version || "-"],
        ];
        if (latest) {
          rows.push(["Load average", [latest.load_1, latest.load_5, latest.load_15].map((l) => l.toFixed(2)).join(" · ")]);
          rows.push(["Uptime", formatDuration(latest.uptime_seconds)]);
        } else if (agent.uptime_seconds !== null) {
          rows.push(["Uptime", formatDuration(agent.uptime_seconds)]);
        }
        return (
          <dl className="facts">
            {rows.map(([name, value]) => (
              <div key={name}>
                <dt>{name}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
        );
      }}
    </AgentView>
  );
}

// CpuWidget shows CPU usage now and over the last minutes.
export function CpuWidget(props: WidgetProps) {
  return (
    <AgentView {...props}>
      {({ agent, points, latest }) =>
        !latest ? (
          <Waiting />
        ) : (
          <>
            <Stat
              value={formatPercent(latest.cpu_percent)}
              caption={`load ${latest.load_1.toFixed(2)} · ${latest.load_5.toFixed(2)} · ${latest.load_15.toFixed(2)}${agent.cpu_count ? ` on ${agent.cpu_count} cores` : ""}`}
            />
            <Chart
              title="CPU usage over the last minutes"
              times={times(points)}
              series={[{ label: "CPU", values: points.map((p) => p.cpu_percent) }]}
              max={100}
              formatValue={(v) => `${v}%`}
            />
          </>
        )
      }
    </AgentView>
  );
}

// MemoryWidget shows memory in use now and over the last minutes, and swap.
export function MemoryWidget(props: WidgetProps) {
  return (
    <AgentView {...props}>
      {({ points, latest }) =>
        !latest ? (
          <Waiting />
        ) : (
          <>
            <Stat
              value={formatPercent(percent(latest.memory_used_bytes, latest.memory_total_bytes))}
              caption={`${formatBytes(latest.memory_used_bytes)} of ${formatBytes(latest.memory_total_bytes)} used${
                latest.swap_total_bytes > 0
                  ? ` · swap ${formatBytes(latest.swap_used_bytes)} of ${formatBytes(latest.swap_total_bytes)}`
                  : ""
              }`}
            />
            <Chart
              title="Memory use over the last minutes"
              times={times(points)}
              series={[{ label: "Memory", values: points.map((p) => percent(p.memory_used_bytes, p.memory_total_bytes)) }]}
              max={100}
              formatValue={(v) => `${v}%`}
            />
          </>
        )
      }
    </AgentView>
  );
}

// DiskWidget shows how full each disk is.
export function DiskWidget(props: WidgetProps) {
  return (
    <AgentView {...props}>
      {({ latest }) => {
        if (!latest) return <Waiting />;
        if (latest.disks.length === 0) return <p>No disks reported.</p>;
        return (
          <ul className="disks">
            {[...latest.disks]
              .sort((a, b) => (a.mount_point < b.mount_point ? -1 : 1))
              .map((d) => {
                const used = percent(d.used_bytes, d.total_bytes);
                return (
                  <li key={d.mount_point}>
                    <div className="disk-head">
                      <strong>{d.mount_point}</strong>
                      <span>
                        {formatBytes(d.used_bytes)} of {formatBytes(d.total_bytes)} ({formatPercent(used)})
                      </span>
                    </div>
                    <div
                      className={`meter${used >= 90 ? " full" : ""}`}
                      role="meter"
                      aria-label={`${d.mount_point} used`}
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-valuenow={Math.round(used)}
                    >
                      <div style={{ width: `${Math.min(used, 100)}%` }} />
                    </div>
                  </li>
                );
              })}
          </ul>
        );
      }}
    </AgentView>
  );
}

// total adds up per-interface rates; null when none could be computed yet.
const total = (rates: (number | null)[]) =>
  rates.some((r) => r !== null) ? rates.reduce<number>((sum, r) => sum + (r ?? 0), 0) : null;

// NetworkWidget shows the traffic of all interfaces, now and over time.
export function NetworkWidget(props: WidgetProps) {
  return (
    <AgentView {...props}>
      {({ points, latest }) => {
        if (!latest) return <Waiting />;
        const down = points.map((p) => total(p.networks.map((n) => n.rx_bytes_per_sec)));
        const up = points.map((p) => total(p.networks.map((n) => n.tx_bytes_per_sec)));
        return (
          <>
            <Stat
              value={`↓ ${formatRate(down.at(-1) ?? 0)}`}
              caption={`↑ ${formatRate(up.at(-1) ?? 0)}`}
            />
            <Chart
              title="Network traffic over the last minutes"
              times={times(points)}
              series={[
                { label: "Received", values: down },
                { label: "Sent", values: up },
              ]}
              minTop={1024}
              formatValue={formatRate}
            />
            {latest.networks.length > 0 && (
              <table>
                <thead>
                  <tr>
                    <th>Interface</th>
                    <th>Received</th>
                    <th>Sent</th>
                  </tr>
                </thead>
                <tbody>
                  {latest.networks.map((n) => (
                    <tr key={n.interface}>
                      <td>{n.interface}</td>
                      <td>{n.rx_bytes_per_sec === null ? "-" : formatRate(n.rx_bytes_per_sec)}</td>
                      <td>{n.tx_bytes_per_sec === null ? "-" : formatRate(n.tx_bytes_per_sec)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        );
      }}
    </AgentView>
  );
}

// UptimeWidget shows how long the server has been running.
export function UptimeWidget(props: WidgetProps) {
  return (
    <AgentView {...props}>
      {({ agent, latest }) => {
        const seconds = latest?.uptime_seconds ?? agent.uptime_seconds;
        if (seconds === null || seconds === undefined) return <Waiting />;
        const since = latest ? Date.parse(latest.at) - seconds * 1000 : null;
        return (
          <Stat
            value={formatDuration(seconds)}
            caption={since === null ? undefined : `since ${new Date(since).toLocaleString()}`}
          />
        );
      }}
    </AgentView>
  );
}
