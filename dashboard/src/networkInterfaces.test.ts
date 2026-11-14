import { describe, expect, it } from "vitest";
import type { NetworkUsage } from "./api";
import { countedInterfaces, isVirtualInterface } from "./networkInterfaces";

const nic = (name: string, rx: number | null = 100): NetworkUsage => ({
  interface: name,
  rx_bytes: 0,
  tx_bytes: 0,
  rx_bytes_per_sec: rx,
  tx_bytes_per_sec: rx,
});

describe("virtual interfaces", () => {
  it("recognises what only repeats the traffic of another interface", () => {
    for (const name of ["docker0", "veth1a2b3c", "br-0f9e8d", "virbr0", "cali1234", "flannel.1", "cni0", "wg0", "tailscale0", "eth0.100", "tap0"]) {
      expect(isVirtualInterface(name), name).toBe(true);
    }
    for (const name of ["eth0", "ens3", "enp0s31f6", "eno1", "wlan0", "wlp2s0", "bond0", "enx001122334455"]) {
      expect(isVirtualInterface(name), name).toBe(false);
    }
  });

  it("counts only the physical interface on a container host", () => {
    const counted = countedInterfaces([nic("eth0"), nic("docker0"), nic("veth1"), nic("veth2"), nic("br-abc")]);
    expect(counted.map((n) => n.interface)).toEqual(["eth0"]);
  });

  it("counts everything when nothing is physical, so the total is not empty", () => {
    expect(countedInterfaces([nic("docker0"), nic("veth1")]).map((n) => n.interface)).toEqual(["docker0", "veth1"]);
    expect(countedInterfaces([])).toEqual([]);
  });
});
