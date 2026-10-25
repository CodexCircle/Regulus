export interface ChartSeries {
  // Name shown in the legend and read by screen readers.
  label: string;
  // One value per time; null leaves a gap.
  values: (number | null)[];
}

interface Props {
  // Description of what the chart shows, for screen readers.
  title: string;
  // When each value was measured, in epoch milliseconds, ascending.
  times: number[];
  series: ChartSeries[];
  // Top of the vertical axis; by default it fits the data.
  max?: number;
  // Smallest top of an axis that fits the data, so that a nearly flat series
  // does not fill the whole height.
  minTop?: number;
  formatValue: (value: number) => string;
  // Only the last windowMs milliseconds are drawn.
  windowMs?: number;
}

export const DEFAULT_WINDOW_MS = 10 * 60 * 1000;

// niceMax rounds a value up to 1, 2 or 5 times a power of ten (and at least
// min), so the axis has a readable top.
export function niceMax(value: number, min = 1): number {
  const target = Math.max(value, min);
  const power = 10 ** Math.floor(Math.log10(target));
  for (const step of [1, 2, 5, 10]) {
    if (target <= step * power) return step * power;
  }
  return 10 * power;
}

// paths builds the line and the filled area of one series. Missing values
// split it into separate runs.
export function paths(times: number[], values: (number | null)[], start: number, end: number, max: number) {
  const span = Math.max(end - start, 1);
  const point = (i: number) => `${(((times[i] - start) / span) * 100).toFixed(2)} ${(100 - (Math.min(values[i] as number, max) / max) * 100).toFixed(2)}`;
  let line = "";
  let area = "";
  let run: number[] = [];
  const flush = () => {
    if (run.length > 0) {
      const first = run[0];
      const last = run[run.length - 1];
      line += run.map((i, n) => `${n === 0 ? "M" : "L"}${point(i)}`).join("");
      area += `M${((times[first] - start) / span * 100).toFixed(2)} 100` + run.map((i) => `L${point(i)}`).join("") + `L${((times[last] - start) / span * 100).toFixed(2)} 100Z`;
    }
    run = [];
  };
  values.forEach((v, i) => {
    if (v === null) flush();
    else run.push(i);
  });
  flush();
  return { line, area };
}

// Chart draws time series as lines with a light fill, in a box that scales to
// its container. Colors come from the --series-N custom properties.
export function Chart({ title, times, series, max, minTop, formatValue, windowMs = DEFAULT_WINDOW_MS }: Props) {
  const end = times.length > 0 ? times[times.length - 1] : 0;
  const from = times.findIndex((t) => t >= end - windowMs);
  if (times.length < 2 || from < 0 || from >= times.length - 1) {
    return <p className="chart-empty">Collecting data…</p>;
  }
  const t = times.slice(from);
  const visible = series.map((s) => ({ ...s, values: s.values.slice(from) }));
  const top = max ?? niceMax(Math.max(0, ...visible.flatMap((s) => s.values.filter((v): v is number => v !== null))), minTop);
  const start = end - windowMs;

  return (
    <figure className="chart">
      <div className="chart-plot">
        <svg viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label={title}>
          {[0, 50, 100].map((y) => (
            <line key={y} className="chart-grid" x1="0" x2="100" y1={y} y2={y} />
          ))}
          {visible.map((s, n) => {
            const d = paths(t, s.values, start, end, top);
            return (
              <g key={s.label} className={`chart-series s${n + 1}`}>
                <path className="chart-area" d={d.area} />
                <path className="chart-line" d={d.line} />
              </g>
            );
          })}
        </svg>
        <span className="chart-max">{formatValue(top)}</span>
        <span className="chart-min">{formatValue(0)}</span>
      </div>
      <figcaption>
        <span>last {Math.round(windowMs / 60000)} min</span>
        {visible.length > 1 &&
          visible.map((s, n) => (
            <span key={s.label} className="chart-legend">
              <i className={`swatch s${n + 1}`} aria-hidden="true" /> {s.label}
            </span>
          ))}
      </figcaption>
    </figure>
  );
}
