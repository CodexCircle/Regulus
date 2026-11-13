import { Chart } from "../Chart";
import { useT } from "../i18n";
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
  const t = useT();
  return (
    <AgentView {...props}>
      {({ agent, stale, latest }) => {
        const rows: [string, string][] = [
          [t("servers.fact.server"), agent.hostname ? `${agent.hostname} (${agent.id})` : agent.id],
          [t("servers.fact.system"), [agent.os_version ?? agent.os, agent.arch].filter(Boolean).join(" ") || "-"],
          [t("servers.fact.kernel"), agent.kernel || "-"],
          [t("servers.fact.cpus"), agent.cpu_count ? String(agent.cpu_count) : "-"],
          [t("servers.fact.memory"), agent.memory_total_bytes ? formatBytes(agent.memory_total_bytes) : "-"],
          [t("servers.fact.status"), t(`servers.status.${stale ? "stale" : agent.status}`)],
          [t("servers.fact.agentVersion"), agent.version || "-"],
        ];
        if (latest) {
          rows.push([t("servers.fact.loadAverage"), [latest.load_1, latest.load_5, latest.load_15].map((l) => l.toFixed(2)).join(" · ")]);
          rows.push([t("servers.fact.uptime"), formatDuration(latest.uptime_seconds)]);
        } else if (agent.uptime_seconds !== null) {
          rows.push([t("servers.fact.uptime"), formatDuration(agent.uptime_seconds)]);
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
  const t = useT();
  return (
    <AgentView {...props}>
      {({ agent, points, latest }) =>
        !latest ? (
          <Waiting />
        ) : (
          <>
            <Stat
              value={formatPercent(latest.cpu_percent)}
              caption={t(agent.cpu_count ? "servers.widget.cpuLoadCores" : "servers.widget.cpuLoad", {
                one: latest.load_1.toFixed(2),
                five: latest.load_5.toFixed(2),
                fifteen: latest.load_15.toFixed(2),
                count: agent.cpu_count ?? 0,
              })}
            />
            <Chart
              title={t("servers.widget.cpuChart")}
              times={times(points)}
              series={[{ label: t("servers.widget.cpuSeries"), values: points.map((p) => p.cpu_percent) }]}
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
  const t = useT();
  return (
    <AgentView {...props}>
      {({ points, latest }) =>
        !latest ? (
          <Waiting />
        ) : (
          <>
            <Stat
              value={formatPercent(percent(latest.memory_used_bytes, latest.memory_total_bytes))}
              caption={t(latest.swap_total_bytes > 0 ? "servers.widget.memoryUsedSwap" : "servers.widget.memoryUsed", {
                used: formatBytes(latest.memory_used_bytes),
                total: formatBytes(latest.memory_total_bytes),
                swapUsed: formatBytes(latest.swap_used_bytes),
                swapTotal: formatBytes(latest.swap_total_bytes),
              })}
            />
            <Chart
              title={t("servers.widget.memoryChart")}
              times={times(points)}
              series={[{ label: t("servers.fact.memory"), values: points.map((p) => percent(p.memory_used_bytes, p.memory_total_bytes)) }]}
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
  const t = useT();
  return (
    <AgentView {...props}>
      {({ latest }) => {
        if (!latest) return <Waiting />;
        if (latest.disks.length === 0) return <p>{t("servers.widget.noDisks")}</p>;
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
                        {t("servers.widget.diskUsage", { used: formatBytes(d.used_bytes), total: formatBytes(d.total_bytes), percent: formatPercent(used) })}
                      </span>
                    </div>
                    <div
                      className={`meter${used >= 90 ? " full" : ""}`}
                      role="meter"
                      aria-label={t("servers.widget.diskMeter", { mount: d.mount_point })}
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
  const t = useT();
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
              title={t("servers.widget.networkChart")}
              times={times(points)}
              series={[
                { label: t("servers.widget.received"), values: down },
                { label: t("servers.widget.sent"), values: up },
              ]}
              minTop={1024}
              formatValue={formatRate}
            />
            {latest.networks.length > 0 && (
              <table>
                <thead>
                  <tr>
                    <th>{t("servers.widget.interface")}</th>
                    <th>{t("servers.widget.received")}</th>
                    <th>{t("servers.widget.sent")}</th>
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
  const t = useT();
  return (
    <AgentView {...props}>
      {({ agent, latest }) => {
        const seconds = latest?.uptime_seconds ?? agent.uptime_seconds;
        if (seconds === null || seconds === undefined) return <Waiting />;
        const since = latest ? Date.parse(latest.at) - seconds * 1000 : null;
        return (
          <Stat
            value={formatDuration(seconds)}
            caption={since === null ? undefined : t("servers.widget.since", { date: new Date(since).toLocaleString() })}
          />
        );
      }}
    </AgentView>
  );
}
