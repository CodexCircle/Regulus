import type { NetworkUsage } from "./api";

// Interfaces that carry traffic which also crosses another interface of the
// same host: container and VM bridges and their ports, tunnels, VLANs
// (eth0.100). Adding them to the physical interface counts the same bytes two
// or three times: a 100 MB/s link would show 300 MB/s on a Docker host.
const VIRTUAL =
  /^(veth|docker|br-|virbr|vnet|vmnet|tap|tun|cali|flannel|cni|cilium|weave|kube|lxc|lxd|podman|wg|tailscale|zt)|\./;

// isVirtualInterface says whether the interface only carries traffic that also
// shows on another one.
export function isVirtualInterface(name: string): boolean {
  return VIRTUAL.test(name);
}

// countedInterfaces is what the total of a server adds up: the physical
// interfaces, or all of them when there is nothing else to count (a server
// that only has virtual interfaces would otherwise show no traffic at all).
// The table still lists every interface.
export function countedInterfaces(networks: NetworkUsage[]): NetworkUsage[] {
  const physical = networks.filter((n) => !isVirtualInterface(n.interface));
  return physical.length > 0 ? physical : networks;
}
