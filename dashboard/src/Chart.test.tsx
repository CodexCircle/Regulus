import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Chart, niceMax, paths } from "./Chart";

describe("niceMax", () => {
  it.each([
    [0, 1, 1],
    [0, 1024, 2000],
    [3, 1, 5],
    [7, 1, 10],
    [11, 1, 20],
    [480, 1, 500],
    [1234, 1, 2000],
    [1_000_000, 1, 1_000_000],
  ])("rounds %d (min %d) up to %d", (value, min, want) => {
    expect(niceMax(value, min)).toBe(want);
  });
});

describe("paths", () => {
  it("scales time to x and the value to y, from the bottom", () => {
    const { line, area } = paths([0, 10], [0, 50], 0, 10, 100);
    expect(line).toBe("M0.00 100.00L100.00 50.00");
    expect(area).toBe("M0.00 100L0.00 100.00L100.00 50.00L100.00 100Z");
  });

  it("splits at gaps and clamps values above the top", () => {
    const { line } = paths([0, 5, 10, 15], [10, null, 200, 10], 0, 15, 100);
    expect(line).toBe("M0.00 90.00M66.67 0.00L100.00 90.00");
  });
});

describe("Chart", () => {
  const fmt = (v: number) => `${v}u`;

  it("waits for two points inside the window", () => {
    render(<Chart title="t" times={[1000]} series={[{ label: "s", values: [1] }]} formatValue={fmt} />);
    expect(screen.getByText("Collecting data…")).toBeInTheDocument();
  });

  it("draws the series with axis labels and a legend for several series", () => {
    render(
      <Chart
        title="Traffic"
        times={[0, 60_000]}
        series={[
          { label: "In", values: [1, 3] },
          { label: "Out", values: [2, 2] },
        ]}
        formatValue={fmt}
        windowMs={120_000}
      />,
    );
    expect(screen.getByRole("img", { name: "Traffic" })).toBeInTheDocument();
    expect(screen.getByText("5u")).toBeInTheDocument(); // top fits the data: 3 -> 5
    expect(screen.getByText("0u")).toBeInTheDocument();
    expect(screen.getByText("last 2 min")).toBeInTheDocument();
    expect(screen.getByText("In")).toBeInTheDocument();
    expect(screen.getByText("Out")).toBeInTheDocument();
  });

  it("leaves out the points older than the window", () => {
    const { container } = render(
      <Chart title="t" times={[0, 400_000, 700_000]} series={[{ label: "s", values: [1, 2, 3] }]} max={10} formatValue={fmt} windowMs={600_000} />,
    );
    // The first point (700 s - 600 s = 100 s limit) is gone; two remain.
    expect(container.querySelector(".chart-line")?.getAttribute("d")?.match(/[ML]/g)).toHaveLength(2);
  });
});
