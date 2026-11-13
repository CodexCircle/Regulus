package capability_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/CodexCircle/Regulus/internal/capability"
)

var presets = []capability.Preset{capability.Compute, capability.Backup, capability.Gateway, capability.Development}

func mustDefaults(t *testing.T, p capability.Preset) capability.Assignment {
	t.Helper()
	a, err := capability.Defaults(p)
	if err != nil {
		t.Fatalf("Defaults(%s): %v", p, err)
	}
	return a
}

func TestEveryPresetHasTheAlwaysOnPieces(t *testing.T) {
	for _, p := range presets {
		a := mustDefaults(t, p)
		if !a.Has(capability.Report) || !a.Has(capability.SelfUpdate) || !a.Has(capability.Inventory) {
			t.Errorf("%s: missing report, self-update or inventory: %v", p, a.Capabilities)
		}
		if !a.Allows(capability.ReachHub) {
			t.Errorf("%s: cannot reach the hub", p)
		}
	}
}

func TestNoPresetReachesPeersOrSharedResourcesByDefault(t *testing.T) {
	// Nodes never contact each other today, and no shared resource exists:
	// both are denied by default for every preset (docs/node-types.md).
	for _, p := range presets {
		a := mustDefaults(t, p)
		if a.Allows(capability.ReachPeers) || a.Allows(capability.SharedResources) {
			t.Errorf("%s: reach-peers or shared-resources on by default: %v", p, a.Permissions)
		}
	}
}

func TestPresetSpecifics(t *testing.T) {
	cases := []struct {
		preset  capability.Preset
		has     []capability.Capability
		hasNot  []capability.Capability
		allows  []capability.Permission
		denies  []capability.Permission
		comment string
	}{
		{capability.Compute, []capability.Capability{capability.Workloads, capability.PluginRuntime, capability.PrivilegedActions},
			[]capability.Capability{capability.BackupStorage, capability.Relay},
			[]capability.Permission{capability.EgressNetwork}, []capability.Permission{capability.InboundListener, capability.ReceiveBackups}, "standard machine"},
		{capability.Backup, []capability.Capability{capability.BackupStorage},
			[]capability.Capability{capability.Workloads, capability.Relay, capability.PrivilegedActions},
			[]capability.Permission{capability.ReceiveBackups}, []capability.Permission{capability.EgressNetwork, capability.InboundListener}, "hub only"},
		{capability.Gateway, []capability.Capability{capability.Relay},
			[]capability.Capability{capability.Notifications, capability.Authentication, capability.Workloads},
			[]capability.Permission{capability.InboundListener}, []capability.Permission{capability.ReceiveBackups}, "notifications and authentication are opt-in"},
		{capability.Development, nil,
			[]capability.Capability{capability.Workloads, capability.PrivilegedActions, capability.Relay, capability.BackupStorage},
			nil, []capability.Permission{capability.EgressNetwork, capability.InboundListener, capability.UsePrivilegedActs}, "isolated"},
	}
	for _, c := range cases {
		a := mustDefaults(t, c.preset)
		for _, x := range c.has {
			if !a.Has(x) {
				t.Errorf("%s (%s): should have %s", c.preset, c.comment, x)
			}
		}
		for _, x := range c.hasNot {
			if a.Has(x) {
				t.Errorf("%s (%s): should not have %s", c.preset, c.comment, x)
			}
		}
		for _, x := range c.allows {
			if !a.Allows(x) {
				t.Errorf("%s (%s): should allow %s", c.preset, c.comment, x)
			}
		}
		for _, x := range c.denies {
			if a.Allows(x) {
				t.Errorf("%s (%s): should not allow %s", c.preset, c.comment, x)
			}
		}
	}
}

func TestDefaultsAreSortedCopies(t *testing.T) {
	a := mustDefaults(t, capability.Compute)
	if !slices.IsSorted(a.Capabilities) || !slices.IsSorted(a.Permissions) {
		t.Errorf("defaults not sorted: %v %v", a.Capabilities, a.Permissions)
	}
	a.Capabilities[0] = "tampered"
	if b := mustDefaults(t, capability.Compute); slices.Contains(b.Capabilities, "tampered") {
		t.Error("changing a result changed the defaults")
	}
}

func TestUnknownPreset(t *testing.T) {
	if _, err := capability.Defaults("nope"); !errors.Is(err, capability.ErrUnknownPreset) {
		t.Errorf("Defaults: %v, want ErrUnknownPreset", err)
	}
	if _, err := capability.Resolve("nope", capability.Override{}); !errors.Is(err, capability.ErrUnknownPreset) {
		t.Errorf("Resolve: %v, want ErrUnknownPreset", err)
	}
}

func TestResolveWithoutOverrideEqualsDefaults(t *testing.T) {
	for _, p := range presets {
		got, err := capability.Resolve(p, capability.Override{})
		if err != nil {
			t.Fatal(err)
		}
		want := mustDefaults(t, p)
		if !slices.Equal(got.Capabilities, want.Capabilities) || !slices.Equal(got.Permissions, want.Permissions) {
			t.Errorf("%s: %v != %v", p, got, want)
		}
	}
}

func TestResolveOneBinaryAnyMachine(t *testing.T) {
	// A standard machine can be given backup storage: it is the same binary.
	a, err := capability.Resolve(capability.Compute, capability.Override{
		Enable: []capability.Capability{capability.BackupStorage, capability.BackupStorage},
		Allow:  []capability.Permission{capability.ReceiveBackups},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Has(capability.BackupStorage) || !a.Allows(capability.ReceiveBackups) || !a.Has(capability.Workloads) {
		t.Errorf("unexpected result: %v", a)
	}
	if !slices.IsSorted(a.Capabilities) || len(slices.Compact(slices.Clone(a.Capabilities))) != len(a.Capabilities) {
		t.Errorf("not sorted or has duplicates: %v", a.Capabilities)
	}
}

func TestResolveRemovingWinsOverAdding(t *testing.T) {
	a, err := capability.Resolve(capability.Compute, capability.Override{
		Enable:  []capability.Capability{capability.Relay},
		Disable: []capability.Capability{capability.Relay, capability.Workloads},
		Deny:    []capability.Permission{capability.EgressNetwork},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Has(capability.Relay) || a.Has(capability.Workloads) || a.Allows(capability.EgressNetwork) {
		t.Errorf("removal did not win: %v", a)
	}
}

func TestResolveRefusals(t *testing.T) {
	cases := []struct {
		name   string
		preset capability.Preset
		o      capability.Override
		want   error
	}{
		{"cannot disable report", capability.Compute, capability.Override{Disable: []capability.Capability{capability.Report}}, capability.ErrFixed},
		{"cannot deny the hub", capability.Backup, capability.Override{Deny: []capability.Permission{capability.ReachHub}}, capability.ErrFixed},
		{"development cannot be a gateway", capability.Development, capability.Override{Enable: []capability.Capability{capability.Relay}}, capability.ErrNotAllowed},
		{"development cannot listen", capability.Development, capability.Override{Allow: []capability.Permission{capability.InboundListener}}, capability.ErrNotAllowed},
		{"development needs a grant for shared resources", capability.Development, capability.Override{Allow: []capability.Permission{capability.SharedResources}}, capability.ErrNeedsGrant},
		{"development needs a grant to reach peers", capability.Development, capability.Override{Allow: []capability.Permission{capability.ReachPeers}}, capability.ErrNeedsGrant},
	}
	for _, c := range cases {
		if _, err := capability.Resolve(c.preset, c.o); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

func TestNeedsConsent(t *testing.T) {
	for _, c := range []capability.Capability{capability.Relay, capability.Notifications, capability.Authentication} {
		if !capability.NeedsConsent(c) {
			t.Errorf("%s should need the machine's consent", c)
		}
	}
	for _, c := range []capability.Capability{capability.Report, capability.SelfUpdate, capability.BackupStorage, capability.Workloads} {
		if capability.NeedsConsent(c) {
			t.Errorf("%s should not need the machine's consent", c)
		}
	}
}
