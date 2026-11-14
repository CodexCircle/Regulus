# Regulus Access (Android)

The guide to the result of the epic [#152](https://github.com/Stellatinus/Regulus/issues/152). It is **written as the design issues close**. Today it is a skeleton: **nothing here is implemented, and most of it is not decided.** Names are provisional.

Status words as in [hub-redundancy.md](hub-redundancy.md): *not designed*, *designed*, *implemented*, *verified*. A design issue of #152 is **not complete** until its section is here and its decision is in [decisions.md](decisions.md). Terms are in the [glossary](glossary.md); node kinds are in [node-types.md](node-types.md).

## 1. Overview

*Status: direction agreed in conversation, not decided in the issues.*

**Regulus Access** is an Android app. It is **not** a mobile Regulus: it has no hub and no agent, and it does not manage the phone. It is a secure access point to a Regulus group: the phone connects to the **hub** (never straight to the other nodes), receives notifications, and can perform actions that the web dashboard may deliberately not offer.

The direction, as it stands:

- **The phone is always a client.** It connects out, so mobile networks and NAT are no problem. Identity is a **device certificate** issued by the group's CA, with the private key in the Android Keystore (mTLS, TLS 1.3). No second layer of encryption is needed.
- **Which address it uses** is an ordered list kept by the hub: the **local** hub address when at home (like Plex, [#162](https://github.com/Stellatinus/Regulus/issues/162)), an optional **overlay** address (Tailscale, Headscale or WireGuard, optional, never required), and the **access gateway** when away.
- **Access gateway** ([#161](https://github.com/Stellatinus/Regulus/issues/161)): a public node that forwards the phone's connection to the active hub through its existing outbound connection. mTLS stays end to end phone to hub, so the gateway sees only encrypted data and holds no secrets. It follows a failover to the redundant hub, so the phone does not change anything.
- **Passkeys** (#50) stay for signing in to the web dashboard; Access uses the device key.

Rejected for the first version: a public Access port on every node, and a mandatory overlay network. Reasoning: a public port per node multiplies the attack surface; an overlay requirement is not needed because the phone-to-hub mTLS is already authenticated and encrypted. An overlay stays a free option for anyone who does not want to expose anything publicly.

## 2. Scope and permissions ([#153](https://github.com/Stellatinus/Regulus/issues/153))

*Status: not designed.* What the phone may do that the web may not; relation to users and roles; step-up for risky actions.

## 3. Identity ([#154](https://github.com/Stellatinus/Regulus/issues/154))

*Status: not designed.* Device key, enrollment by QR code, revocation of a lost phone, certificate renewal, threat model.

## 4. Reachability and the gateway ([#155](https://github.com/Stellatinus/Regulus/issues/155), [#161](https://github.com/Stellatinus/Regulus/issues/161), [#162](https://github.com/Stellatinus/Regulus/issues/162))

*Status: direction above, not designed.* Address list, the relay's exposure analysis, local discovery, optional overlay. The gateway's capabilities: relay (first version), notifications (second), authentication (later, separate design).

## 5. Protocol and authorization ([#156](https://github.com/Stellatinus/Regulus/issues/156))

*Status: not designed.* A separate `AccessService`, terminated at the hub; approvals; versioning (apps update on their own schedule).

## 6. Notifications ([#157](https://github.com/Stellatinus/Regulus/issues/157))

*Status: not designed.* Events, delivery (UnifiedPush or ntfy against Firebase), push as a wake-up without secrets, approvals from the notification. One provider of the out-of-band channel of #112.

## 7. Operating when the active hub is down ([#158](https://github.com/Stellatinus/Regulus/issues/158))

*Status: not designed.* With a gateway and a **redundant hub** (#103), the phone reaches whichever hub is active, so promotion and failover from the phone become possible. Without a redundant hub there is nobody to connect to, and that case is out of the first version. Must not create a second active hub (#108).

## 8. The app ([#159](https://github.com/Stellatinus/Regulus/issues/159))

*Status: not designed.* Stack (Kotlin and Compose proposed), repository layout, signing, distribution, English and Italian.

## 9. Operations

*Status: not written.* Written after implementation, with the real commands, and run by someone other than the author.
