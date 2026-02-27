RFC-UL-0012

Title: Node Discovery & Cluster Formation
Status: Draft
Authors: Underleaf Core Team
Last Updated: 2026-02-02
Scope: Product behavior and user experience (non-implementation)

⸻

1. Abstract

This RFC defines the default behavior, flows, and invariants for how Underleaf discovers edge nodes on local networks and how clusters are formed.

The goal is to minimize user cognitive load while preserving explicit trust boundaries, preventing accidental topology merges, and maintaining predictable cluster semantics across heterogeneous edge environments.

⸻

2. Goals

Underleaf SHALL:
	1.	Reduce user setup friction for multi-node edge deployments.
	2.	Prevent accidental or unauthorized cluster formation.
	3.	Preserve explicit user intent for all trust relationships.
	4.	Support edge environments with:
	•	Unreliable networks
	•	Small node counts (1–3 common)
	•	Mixed trust LANs (home, lab, office, field)
	5.	Maintain a simple mental model:
	•	Discovery is automatic.
	•	Membership is intentional.

⸻

3. Non-Goals

This RFC does NOT define:
	•	Cluster consensus protocols (Raft, etcd-like stores)
	•	Transport security implementation details
	•	Specific cryptographic primitives
	•	Node scheduling or orchestration behavior

⸻

4. Terminology

Node

An individual Underleaf agent instance running on an edge device.

Standalone Workspace

Default operating mode of a node not associated with a cluster.

Cluster

A logical grouping of nodes sharing configuration state, metadata, and coordination primitives.

Discovery Candidate

A node visible via LAN discovery but not yet trusted.

Trust Ceremony

A short-lived explicit verification step establishing mutual trust between nodes.

⸻

5. Design Principles

5.1 Explicit Trust Over Implicit Network Assumptions

LAN proximity SHALL NOT imply trust.

Physical or network adjacency is treated as a discovery convenience only.

⸻

5.2 Opinionated Defaults

Underleaf defaults SHALL favor:
	•	Safety over automation
	•	Predictability over magic behavior
	•	Minimal steps without hidden side effects

⸻

5.3 Progressive Disclosure

Advanced automation modes MAY exist but MUST require explicit user opt-in.

⸻

6. Default Behavior

6.1 Node Startup

When a node starts for the first time:
	•	It SHALL initialize as a Standalone Workspace
	•	It SHALL NOT automatically join or create clusters
	•	It SHALL advertise itself as discoverable on the LAN

⸻

6.2 LAN Discovery

Underleaf SHALL:
	•	Automatically discover nearby nodes using mDNS or equivalent mechanisms
	•	Display discovered nodes as Discovery Candidates
	•	Clearly label them as:
	•	“Untrusted”
	•	“Not part of your cluster”

Discovery SHALL be enabled by default.

⸻

6.3 Cluster Formation Defaults

Underleaf SHALL NOT automatically form or join clusters.

Cluster creation or joining SHALL require:
	•	Explicit user action
	•	A trust ceremony step

⸻

7. User Flows

⸻

7.1 Create New Cluster (First Node)

Trigger:
User selects “Create Cluster” from UI or CLI.

Behavior:
	1.	Current node becomes cluster root
	2.	Cluster identity is created
	3.	Join credentials are generated
	4.	Node transitions from Standalone → Cluster Member

⸻

7.2 Join Existing Cluster (Manual)

Trigger:
User selects a Discovery Candidate or provides join credentials.

Flow:
	1.	User initiates join
	2.	System presents trust ceremony:
	•	Short verification code
	•	QR code
	•	Fingerprint comparison
	3.	User confirms match
	4.	Mutual trust is established
	5.	Node joins cluster

**Post-Join Behavior (Config Reconciliation):**
Once a node successfully joins a cluster, the Control Plane publishes a `cluster.membership.changed` event via Mycelium Spine. Upon receiving this event, the agent MUST immediately trigger a configuration reconciliation (`PolicyManager.Reconcile()`). This ensures the agent promptly fetches its updated configuration—which now includes the `raft` section with `bootstrap_peers`—allowing the local Raft node to initialize and participate in leader election without waiting for the next periodic sync interval.

⸻

7.3 Assisted Discovery Join

When LAN nodes are detected:

UI SHALL present:
	•	Nearby nodes list
	•	Primary call-to-action:
	•	“Add to Cluster”
	•	“Form Cluster With These Nodes”

This preserves speed without silent behavior.

⸻

7.4 CLI Flow (Canonical)

Example flows:

ufctl discover
ufctl cluster init
ufctl cluster join <node-id> --code XXXX-XXXX

CLI SHALL provide parity with UI flows.

⸻

8. Optional Automation Mode (Advanced)

8.1 Auto-Join Mode

Underleaf MAY support optional auto-join behavior with the following constraints:

Auto-join is permitted ONLY if:
	•	A valid cluster join token is preconfigured
	•	Node explicitly opts into auto-join mode
	•	Token matches advertised cluster identity

Auto-join SHALL NOT:
	•	Trust arbitrary LAN nodes
	•	Bypass authentication
	•	Join clusters without preauthorization

⸻

8.2 Intended Use Cases

Auto-join mode is designed for:
	•	Pre-provisioned factory images
	•	Batch hardware deployments
	•	Fleet bootstrap scenarios

⸻

9. Security Invariants

The following invariants MUST hold at all times:

⸻

9.1 No Implicit Trust

Underleaf SHALL NEVER:
	•	Auto-cluster based on LAN presence alone
	•	Assume mDNS identity authenticity
	•	Merge clusters without explicit authorization

⸻

9.2 Mutual Authentication

All cluster membership SHALL require:
	•	Bidirectional verification
	•	Cryptographic trust establishment

⸻

9.3 Reversible Membership

Cluster membership SHALL be revocable:
	•	Node can be removed
	•	Credentials invalidated
	•	State replication halted immediately

⸻

9.4 Least Surprise Principle

Users SHALL always be able to answer:
	•	Why is this node in this cluster?
	•	When did it join?
	•	Who authorized it?

⸻

10. Edge Case Handling

⸻

10.1 Duplicate Node Names

Nodes SHALL be uniquely identified internally.

UI SHOULD disambiguate using:
	•	Short IDs
	•	Hardware fingerprints
	•	Network info

⸻

10.2 Network Partitions

Temporary LAN visibility loss SHALL NOT:
	•	Remove nodes from clusters
	•	Trigger re-clustering
	•	Create duplicate memberships

⸻

10.3 Multiple Clusters on Same LAN

Underleaf SHALL:
	•	Treat each cluster as isolated
	•	Never auto-merge clusters
	•	Require explicit bridging if ever supported

⸻

10.4 Reinstalled Nodes

If a node is reinstalled:
	•	Previous trust SHALL be invalidated
	•	Rejoin requires new authorization

⸻

11. UX Requirements

Underleaf UI SHALL:
	•	Clearly separate:
	•	“Nearby Nodes”
	•	“Cluster Members”
	•	Visually mark trust state
	•	Surface cluster formation as a first-class onboarding action

⸻

12. Telemetry & Observability (Optional)

Underleaf MAY track:
	•	Discovery frequency
	•	Cluster join attempts
	•	Failed trust ceremonies

For improving UX only — never auto-behavior.

⸻

13. Summary

Underleaf adopts the principle:

Automatic discovery. Explicit membership.

This preserves safety, reduces support complexity, avoids silent topology mistakes, and aligns with Underleaf’s role as an opinionated edge infrastructure platform.
