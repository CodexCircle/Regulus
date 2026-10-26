import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RolloutStatus, UpdateStatus, User } from "./api";
import { SessionContext } from "./session";
import { ADMIN_PERMISSIONS } from "./test-users";
import { UpdatesPage } from "./UpdatesPage";

const now = new Date().toISOString();
const admin: User = {
  username: "admin",
  role: "admin",
  permissions: ADMIN_PERMISSIONS,
  created_at: now,
  disabled: false,
  two_factor: false,
};

const base: UpdateStatus = {
  current_version: "0.1.0",
  release_build: true,
  checks_enabled: true,
  mode: "manual",
  channel: "stable",
  platform: "linux/amd64",
  checked_at: now,
  checking: false,
  latest: null,
  update_available: false,
  staged: null,
  downloading: false,
  apply_enabled: false,
  apply_available: false,
  applying: false,
  last_apply: null,
};

const newer: UpdateStatus = {
  ...base,
  latest: {
    version: "0.2.0",
    tag: "v0.2.0",
    prerelease: false,
    url: "https://github.com/CodexCircle/Regulus/releases/tag/v0.2.0",
    notes: "Adds updates.",
    published_at: now,
  },
  update_available: true,
};

const staged: UpdateStatus = {
  ...newer,
  staged: {
    version: "0.2.0",
    tag: "v0.2.0",
    archive: "regulus_0.2.0_linux_amd64.tar.gz",
    sha256: "ab".repeat(32),
    size: 12 * 1024 * 1024,
    verified: "checksum",
    staged_at: now,
    signed: true,
  },
};

const idle: RolloutStatus = {
  target: "0.2.0",
  state: "idle",
  candidates: [],
  agents: [],
  started_at: null,
  finished_at: null,
};
const web1 = { id: "agent-1", hostname: "web1", from: "0.1.0" } as const;
const web2 = { id: "agent-2", hostname: "web2", from: "0.1.0" } as const;

interface Call {
  method: string;
  path: string;
  body: unknown;
}

// mockHub answers each request with the next response queued for "METHOD path"
// (the last one repeats).
function mockHub(
  given: Record<string, { status?: number; data: unknown }[]>,
): Call[] {
  const responses: Record<string, { status?: number; data: unknown }[]> = {
    "GET /api/updates/rollout": [{ data: idle }],
    ...given,
  };
  const calls: Call[] = [];
  const index: Record<string, number> = {};
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const key = `${method} ${path}`;
      calls.push({
        method,
        path,
        body: init?.body
          ? (JSON.parse(init.body as string) as unknown)
          : undefined,
      });
      const queue = responses[key] ?? [
        { status: 404, data: { error: "not found" } },
      ];
      const i = Math.min(index[key] ?? 0, queue.length - 1);
      index[key] = (index[key] ?? 0) + 1;
      const { status = 200, data } = queue[i];
      return Promise.resolve({
        ok: status < 400,
        status,
        json: () => Promise.resolve(data),
      });
    }),
  );
  return calls;
}

function renderPage() {
  return render(
    <SessionContext.Provider
      value={{
        user: admin,
        signOut: () => Promise.resolve(),
        expired: vi.fn(),
      }}
    >
      <UpdatesPage />
    </SessionContext.Provider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("UpdatesPage", () => {
  it("says Regulus is up to date", async () => {
    mockHub({
      "GET /api/updates": [
        { data: { ...base, latest: { ...newer.latest!, version: "0.1.0" } } },
      ],
    });
    renderPage();
    expect(
      await screen.findByText(
        /Regulus is up to date: the newest stable release is 0.1.0/,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("0.1.0", { selector: "strong" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Download/ }),
    ).not.toBeInTheDocument();
  });

  it("offers a newer release with its notes and a link", async () => {
    mockHub({ "GET /api/updates": [{ data: newer }] });
    renderPage();
    const box = await screen.findByRole("status", { name: "Update available" });
    expect(within(box).getByText("Version 0.2.0")).toBeInTheDocument();
    expect(within(box).getByText("Adds updates.")).toBeInTheDocument();
    expect(
      within(box).getByRole("link", { name: "Release page" }),
    ).toHaveAttribute("href", newer.latest!.url);
    expect(
      screen.getByRole("button", { name: "Download 0.2.0" }),
    ).toBeEnabled();
  });

  it("does not link a release page that is not https", async () => {
    mockHub({
      "GET /api/updates": [
        {
          data: {
            ...newer,
            latest: { ...newer.latest!, url: "javascript:alert(1)" },
          },
        },
      ],
    });
    renderPage();
    await screen.findByRole("status", { name: "Update available" });
    expect(
      screen.queryByRole("link", { name: "Release page" }),
    ).not.toBeInTheDocument();
  });

  it("checks now and shows the answer", async () => {
    const calls = mockHub({
      "GET /api/updates": [{ data: base }],
      "POST /api/updates/check": [{ data: newer }],
    });
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Check now" }),
    );
    expect(
      await screen.findByRole("status", { name: "Update available" }),
    ).toBeInTheDocument();
    expect(calls).toContainEqual({
      method: "POST",
      path: "/api/updates/check",
      body: {},
    });
  });

  it("shows why a check failed, from the answer and from the status", async () => {
    mockHub({
      "GET /api/updates": [
        { data: base },
        {
          data: {
            ...base,
            check_error: "the release server's request limit is used up",
          },
        },
      ],
      "POST /api/updates/check": [
        {
          status: 502,
          data: { error: "the release server's request limit is used up" },
        },
      ],
    });
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Check now" }),
    );
    const alerts = await screen.findAllByRole("alert");
    expect(alerts.map((a) => a.textContent).join("\n")).toMatch(
      /request limit is used up/,
    );
    expect(
      await screen.findByText(/The last check failed/),
    ).toBeInTheDocument();
  });

  it("downloads, follows the progress and says it is not installed", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const calls = mockHub({
      "GET /api/updates": [
        { data: newer },
        { data: { ...newer, downloading: true } },
        { data: staged },
      ],
      "POST /api/updates/download": [
        { status: 202, data: { ...newer, downloading: true } },
      ],
    });
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Download 0.2.0" }),
    );
    expect(await screen.findByText("Downloading…")).toBeInTheDocument();
    // The hub is still downloading at the first poll and done at the second.
    await vi.advanceTimersByTimeAsync(2100);
    await vi.advanceTimersByTimeAsync(2100);
    const box = await screen.findByRole("status", {
      name: "Downloaded update",
    });
    expect(box).toHaveTextContent("12.0 MiB");
    expect(box).toHaveTextContent("not installed");
    expect(box).toHaveTextContent("was not set up to install updates itself");
    expect(
      screen.queryByRole("button", { name: /^Download/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Install/ }),
    ).not.toBeInTheDocument();
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(1);
  });

  it("says when the signature of the download was checked", async () => {
    mockHub({
      "GET /api/updates": [
        {
          data: {
            ...staged,
            staged: { ...staged.staged!, verified: "signature" },
          },
        },
      ],
    });
    renderPage();
    const box = await screen.findByRole("status", {
      name: "Downloaded update",
    });
    expect(box).toHaveTextContent("Its signature was checked");
    expect(box).not.toHaveTextContent("does not prove who published it");
  });

  it("shows a failed download", async () => {
    mockHub({
      "GET /api/updates": [
        {
          data: {
            ...newer,
            download_error:
              "regulus_0.2.0_linux_amd64.tar.gz does not match its checksum",
          },
        },
      ],
    });
    renderPage();
    expect(
      await screen.findByText(
        /The download failed: .*does not match its checksum/,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Download 0.2.0" }),
    ).toBeEnabled();
  });

  it("explains a development build and a hub with checks off", async () => {
    mockHub({
      "GET /api/updates": [
        {
          data: {
            ...base,
            current_version: "dev",
            release_build: false,
            checks_enabled: false,
            checked_at: null,
          },
        },
      ],
    });
    renderPage();
    expect(await screen.findByText(/development build/)).toBeInTheDocument();
    expect(
      screen.getByText(/turned off in the configuration/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Check now" })).toBeDisabled();
  });

  it("saves the mode and the channel", async () => {
    const calls = mockHub({
      "GET /api/updates": [{ data: base }],
      "PUT /api/updates/settings": [
        { data: { ...base, mode: "download", channel: "beta" } },
      ],
    });
    renderPage();
    const save = await screen.findByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
    await userEvent.click(
      screen.getByRole("radio", {
        name: "Download and check it, then wait for me",
      }),
    );
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Channel" }),
      "beta",
    );
    await userEvent.click(save);
    await waitFor(() =>
      expect(calls).toContainEqual({
        method: "PUT",
        path: "/api/updates/settings",
        body: { mode: "download", channel: "beta" },
      }),
    );
    // The form follows the saved state and has nothing left to save.
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Save" })).toBeDisabled(),
    );
    expect(
      screen.getByRole("radio", {
        name: "Download and check it, then wait for me",
      }),
    ).toBeChecked();
  });

  it("shows an error when the status cannot be loaded", async () => {
    mockHub({
      "GET /api/updates": [{ status: 500, data: { error: "internal error" } }],
    });
    renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "internal error",
    );
  });

  const ready: UpdateStatus = {
    ...staged,
    staged: { ...staged.staged!, verified: "signature" },
    apply_enabled: true,
    apply_available: true,
  };

  it("installs after a confirmation and follows the restart", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const calls = mockHub({
      "GET /api/updates": [
        { data: ready },
        { data: { ...ready, applying: true, apply_available: false } },
        { status: 502, data: { error: "bad gateway" } },
        {
          data: {
            ...base,
            current_version: "0.2.0",
            apply_enabled: true,
            last_apply: {
              version: "0.2.0",
              from: "0.1.0",
              outcome: "applied",
              at: now,
            },
          },
        },
      ],
      "POST /api/updates/apply": [
        {
          status: 202,
          data: { ...ready, applying: true, apply_available: false },
        },
      ],
    });
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Install 0.2.0" }),
    );
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(0);
    expect(
      screen.getByRole("group", { name: "Confirm installation" }),
    ).toHaveTextContent("restarts");
    await userEvent.click(screen.getByRole("button", { name: "Install now" }));
    expect(await screen.findByText(/Installing 0.2.0…/)).toBeInTheDocument();
    expect(calls).toContainEqual({
      method: "POST",
      path: "/api/updates/apply",
      body: {},
    });
    // The hub goes away for a moment and comes back with the new version.
    await vi.advanceTimersByTimeAsync(2100);
    await vi.advanceTimersByTimeAsync(2100);
    await vi.advanceTimersByTimeAsync(2100);
    expect(
      await screen.findByText(/Updated to 0.2.0 from 0.1.0/),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Version" })).getByText(
        "0.2.0",
        { selector: "strong" },
      ),
    ).toBeInTheDocument();
  });

  it("can be cancelled before anything is sent", async () => {
    const calls = mockHub({ "GET /api/updates": [{ data: ready }] });
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Install 0.2.0" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen.queryByRole("group", { name: "Confirm installation" }),
    ).not.toBeInTheDocument();
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(0);
  });

  it("does not offer to install a release that was not verified by its signature", async () => {
    mockHub({
      "GET /api/updates": [{ data: { ...ready, apply_available: false } }],
    });
    renderPage();
    await screen.findByRole("status", { name: "Downloaded update" });
    expect(
      screen.queryByRole("button", { name: /^Install/ }),
    ).not.toBeInTheDocument();
  });

  it("says a version was rolled back, and when nothing picks the request up", async () => {
    mockHub({
      "GET /api/updates": [
        {
          data: {
            ...base,
            apply_enabled: true,
            last_apply: {
              version: "0.2.0",
              from: "0.1.0",
              outcome: "rolled_back",
              message: "the new version did not come up",
              at: now,
            },
          },
        },
      ],
    });
    renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Version 0.2.0 did not start properly, so the previous version was put back. the new version did not come up/,
    );
    vi.unstubAllGlobals();
    mockHub({
      "GET /api/updates": [
        { data: { ...ready, applying: true, apply_stalled: true } },
      ],
    });
    renderPage();
    expect(
      await screen.findByText(/systemctl enable --now regulus-update.path/),
    ).toBeInTheDocument();
  });

  it("saves the automatic mode with its maintenance window", async () => {
    const calls = mockHub({
      "GET /api/updates": [{ data: { ...base, apply_enabled: true } }],
      "PUT /api/updates/settings": [
        {
          data: {
            ...base,
            apply_enabled: true,
            mode: "automatic",
            window_start: "02:30",
            window_end: "04:00",
          },
        },
      ],
    });
    renderPage();
    await userEvent.click(
      await screen.findByRole("radio", { name: /install it by itself/ }),
    );
    const from = screen.getByLabelText("Window starts");
    const to = screen.getByLabelText("Window ends");
    fireEvent.change(from, { target: { value: "02:30" } });
    fireEvent.change(to, { target: { value: "04:00" } });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls).toContainEqual({
        method: "PUT",
        path: "/api/updates/settings",
        body: {
          mode: "automatic",
          channel: "stable",
          window_start: "02:30",
          window_end: "04:00",
        },
      }),
    );
  });

  it("does not offer the automatic mode where installing is not set up", async () => {
    mockHub({ "GET /api/updates": [{ data: base }] });
    renderPage();
    expect(
      await screen.findByRole("radio", { name: /install it by itself/ }),
    ).toBeDisabled();
    expect(
      screen.getByText(/Installing is not set up on this hub/),
    ).toBeInTheDocument();
  });

  it("updates the servers that are behind, a wave at a time", async () => {
    const calls = mockHub({
      "GET /api/updates": [{ data: base }],
      "GET /api/updates/rollout": [
        {
          data: {
            ...idle,
            candidates: [
              { ...web1, status: "pending" },
              { ...web2, status: "pending" },
            ],
          },
        },
      ],
      "POST /api/updates/rollout": [
        {
          status: 202,
          data: {
            ...idle,
            state: "running",
            wave_size: 3,
            agents: [
              { ...web1, status: "offered" },
              { ...web2, status: "pending" },
            ],
          },
        },
      ],
    });
    renderPage();
    expect(
      await screen.findByText("2 servers run an older version."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("list", { name: "Servers behind" }),
    ).toHaveTextContent("web1 runs 0.1.0");
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "At a time" }),
      "3",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Update 2 servers to 0.2.0" }),
    );
    await waitFor(() =>
      expect(calls).toContainEqual({
        method: "POST",
        path: "/api/updates/rollout",
        body: { wave_size: 3 },
      }),
    );
    const progress = await screen.findByRole("list", {
      name: "Update progress",
    });
    expect(progress).toHaveTextContent("web1 (0.1.0): fetching the update");
    expect(progress).toHaveTextContent("web2 (0.1.0): waiting");
    expect(screen.getByText(/3 at a time/)).toBeInTheDocument();
  });

  it("follows the rollout to the end and shows what failed", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const running: RolloutStatus = {
      ...idle,
      state: "running",
      wave_size: 1,
      agents: [
        { ...web1, status: "installing" },
        { ...web2, status: "pending" },
      ],
    };
    const failed: RolloutStatus = {
      ...idle,
      state: "failed",
      finished_at: now,
      agents: [
        {
          ...web1,
          status: "failed",
          message:
            "the server came back with version 0.1.0: the update did not take and was rolled back",
        },
        { ...web2, status: "skipped" },
      ],
    };
    mockHub({
      "GET /api/updates": [{ data: base }],
      "GET /api/updates/rollout": [{ data: running }, { data: failed }],
    });
    renderPage();
    expect(
      await screen.findByRole("list", { name: "Update progress" }),
    ).toHaveTextContent("installing, waiting for it to come back");
    await vi.advanceTimersByTimeAsync(2100);
    const last = await screen.findByRole("list", { name: "Last update" });
    expect(last).toHaveTextContent(
      "web1 (0.1.0): failed — the server came back with version 0.1.0",
    );
    expect(last).toHaveTextContent("web2 (0.1.0): not tried");
    expect(screen.getByText(/stopped by a failure/)).toBeInTheDocument();
  });

  it("stops a rollout", async () => {
    const running: RolloutStatus = {
      ...idle,
      state: "running",
      wave_size: 1,
      agents: [{ ...web1, status: "installing" }],
    };
    const calls = mockHub({
      "GET /api/updates": [{ data: base }],
      "GET /api/updates/rollout": [{ data: running }],
      "POST /api/updates/rollout/stop": [
        {
          data: {
            ...idle,
            state: "stopped",
            finished_at: now,
            agents: [{ ...web1, status: "failed", message: "stopped" }],
          },
        },
      ],
    });
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Stop" }));
    await waitFor(() =>
      expect(calls).toContainEqual({
        method: "POST",
        path: "/api/updates/rollout/stop",
        body: {},
      }),
    );
    expect(
      await screen.findByText(/Last update of the servers: stopped/),
    ).toBeInTheDocument();
  });

  it("says when no server is behind", async () => {
    mockHub({ "GET /api/updates": [{ data: base }] });
    renderPage();
    expect(
      await screen.findByText(/Every connected server runs 0.2.0/),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Update/ }),
    ).not.toBeInTheDocument();
  });
});
