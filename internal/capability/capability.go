// Package capability is the draft data model for what an agent machine does
// (its capabilities) and what it may reach or use (its permissions), with the
// defaults of each preset (compute, backup, gateway, development) and the rules
// for overriding them per machine. Issues #169 (capabilities), #170
// (permissions) and #168 (the development preset) design it; the intended use
// is described in docs/node-types.md once that page is merged.
//
// It does not touch the database, the protocol, the HTTP API or any running
// code: nothing calls it yet, and no decision recorded in docs/decisions.md
// depends on it. These are not the user permissions of internal/auth (what a
// person may do on the hub). Names are provisional.
package capability

import (
	"errors"
	"fmt"
	"slices"
)

// Capability is a function a machine does.
type Capability string

// The capabilities listed in #169. Exposure-changing ones need the machine's
// own consent as well as the hub's grant (see NeedsConsent).
const (
	Report            Capability = "report"             // system information, heartbeat, metrics: always on
	SelfUpdate        Capability = "self-update"        // #18
	Inventory         Capability = "inventory"          // read-only machine information, #62
	Workloads         Capability = "workloads"          // #113
	PrivilegedActions Capability = "privileged-actions" // #60
	PluginRuntime     Capability = "plugin-runtime"     // #19
	BackupStorage     Capability = "backup-storage"     // #166
	Relay             Capability = "relay"              // access gateway, #161
	Notifications     Capability = "notifications"      // access gateway, #161
	Authentication    Capability = "authentication"     // access gateway, #161
)

// Permission is something a machine may reach or use.
type Permission string

// The permissions listed in #170.
const (
	ReachHub          Permission = "reach-hub"          // always allowed
	ReachPeers        Permission = "reach-peers"        // direct links to other machines, #113; none exist today
	SharedResources   Permission = "shared-resources"   // what the hub holds for everyone; none exist yet
	EgressNetwork     Permission = "egress-network"     // reaching the network outside the group
	InboundListener   Permission = "inbound-listener"   // accepting connections
	ReceiveBackups    Permission = "receive-backups"    // #166
	UsePrivilegedActs Permission = "privileged-actions" // #60
)

// Preset is a named starting set of capabilities and permissions for an agent.
// The hub and the redundant hub are roles (#104), not presets.
type Preset string

// The presets of docs/node-types.md.
const (
	Compute     Preset = "compute"
	Backup      Preset = "backup"
	Gateway     Preset = "gateway"
	Development Preset = "development"
)

var (
	// ErrUnknownPreset is returned for a preset that does not exist.
	ErrUnknownPreset = errors.New("unknown preset")
	// ErrFixed is returned when an override tries to remove a capability or
	// permission that is always on (Report, ReachHub).
	ErrFixed = errors.New("always on, cannot be removed")
	// ErrNeedsGrant is returned when an override tries to give a development
	// machine a permission that is only available through a delayed grant
	// (#168), never as a setting.
	ErrNeedsGrant = errors.New("only available through a grant")
	// ErrNotAllowed is returned when a preset may never have a capability or
	// permission, whatever the override says.
	ErrNotAllowed = errors.New("not allowed for this preset")
)

// Assignment is what a machine ends up with. Both lists are sorted and
// contain no duplicates, so two equal assignments compare equal.
type Assignment struct {
	Capabilities []Capability
	Permissions  []Permission
}

// Has reports whether the assignment includes c.
func (a Assignment) Has(c Capability) bool { return slices.Contains(a.Capabilities, c) }

// Allows reports whether the assignment includes p.
func (a Assignment) Allows(p Permission) bool { return slices.Contains(a.Permissions, p) }

// Override changes a preset's defaults for one machine. Deny and Disable are
// applied after Allow and Enable, so removing wins over adding.
type Override struct {
	Enable  []Capability
	Disable []Capability
	Allow   []Permission
	Deny    []Permission
}

// NeedsConsent reports whether turning c on also needs the machine's own
// consent (a local, default-off setting), because it changes what the machine
// exposes. The hub alone must not be able to make a server open a port (#160,
// #164).
func NeedsConsent(c Capability) bool {
	switch c {
	case Relay, Notifications, Authentication:
		return true
	}
	return false
}

var defaults = map[Preset]Assignment{
	Compute: {
		Capabilities: []Capability{Report, SelfUpdate, Inventory, Workloads, PrivilegedActions, PluginRuntime},
		Permissions:  []Permission{ReachHub, EgressNetwork, UsePrivilegedActs},
	},
	Backup: {
		Capabilities: []Capability{Report, SelfUpdate, Inventory, BackupStorage},
		Permissions:  []Permission{ReachHub, ReceiveBackups},
	},
	Gateway: {
		Capabilities: []Capability{Report, SelfUpdate, Inventory, Relay},
		Permissions:  []Permission{ReachHub, InboundListener},
	},
	Development: {
		// Workloads and the network outside the group are limited in a way
		// that is still open in #168, so they are not on by default.
		Capabilities: []Capability{Report, SelfUpdate, Inventory},
		Permissions:  []Permission{ReachHub},
	},
}

// forbidden lists what a preset may never have through an override.
var forbidden = map[Preset]struct {
	caps  []Capability
	perms []Permission
}{
	Development: {
		caps:  []Capability{Relay, Notifications, Authentication, BackupStorage, PrivilegedActions},
		perms: []Permission{InboundListener, ReceiveBackups, UsePrivilegedActs},
	},
}

// grantOnly lists what a preset may have only through a grant, never an override.
var grantOnly = map[Preset][]Permission{
	Development: {ReachPeers, SharedResources},
}

// Defaults returns the capabilities and permissions a preset starts with. The
// result is a copy; changing it does not change the defaults.
func Defaults(p Preset) (Assignment, error) {
	d, ok := defaults[p]
	if !ok {
		return Assignment{}, fmt.Errorf("%w: %q", ErrUnknownPreset, p)
	}
	a := Assignment{
		Capabilities: slices.Clone(d.Capabilities),
		Permissions:  slices.Clone(d.Permissions),
	}
	slices.Sort(a.Capabilities)
	slices.Sort(a.Permissions)
	return a, nil
}

// Resolve applies an override to a preset's defaults and returns the result.
// It refuses an override that removes what is always on (ErrFixed), gives a
// development machine a grant-only permission (ErrNeedsGrant) or a forbidden
// one (ErrNotAllowed).
func Resolve(p Preset, o Override) (Assignment, error) {
	base, err := Defaults(p)
	if err != nil {
		return Assignment{}, err
	}
	if slices.Contains(o.Disable, Report) {
		return Assignment{}, fmt.Errorf("%w: %s", ErrFixed, Report)
	}
	if slices.Contains(o.Deny, ReachHub) {
		return Assignment{}, fmt.Errorf("%w: %s", ErrFixed, ReachHub)
	}
	for _, c := range o.Enable {
		if slices.Contains(forbidden[p].caps, c) {
			return Assignment{}, fmt.Errorf("%w: capability %s on %s", ErrNotAllowed, c, p)
		}
	}
	for _, perm := range o.Allow {
		if slices.Contains(grantOnly[p], perm) {
			return Assignment{}, fmt.Errorf("%w: permission %s on %s", ErrNeedsGrant, perm, p)
		}
		if slices.Contains(forbidden[p].perms, perm) {
			return Assignment{}, fmt.Errorf("%w: permission %s on %s", ErrNotAllowed, perm, p)
		}
	}

	caps := append(base.Capabilities, o.Enable...)
	caps = slices.DeleteFunc(caps, func(c Capability) bool { return slices.Contains(o.Disable, c) })
	perms := append(base.Permissions, o.Allow...)
	perms = slices.DeleteFunc(perms, func(perm Permission) bool { return slices.Contains(o.Deny, perm) })

	slices.Sort(caps)
	slices.Sort(perms)
	return Assignment{Capabilities: slices.Compact(caps), Permissions: slices.Compact(perms)}, nil
}
