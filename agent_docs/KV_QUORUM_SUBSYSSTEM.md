Title: Edge-Optimized Raft Quorum and Configuration Store
Status: Draft
Author: Ambient Labs / Underleaf Platform
Target Version: v1

⸻

1. Purpose

This document specifies the behavioral and logical requirements for an embedded Raft-based consensus subsystem intended for edge clusters with constrained resources and small node counts.

The system provides:
	•	Strongly consistent coordination primitives
	•	A small, durable configuration and metadata store
	•	Safe leader election and membership management
	•	A stable control plane nucleus for edge orchestration

This RFC defines observable behavior and guarantees, not implementation details.

⸻

2. Non-Goals

This system is explicitly NOT:
	•	A general-purpose database
	•	A telemetry, logging, or metrics store
	•	A bulk artifact or blob store
	•	A global multi-region replication engine
	•	A high-throughput transactional system

⸻

3. Design Principles

The system SHALL prioritize:
	1.	Safety over availability
	2.	Deterministic behavior over dynamic automation
	3.	Small cluster optimization
	4.	Operational simplicity
	5.	Explicit operator intent

⸻

4. Cluster Size Model (Edge-First)

Edge deployments commonly operate with minimal node counts. The system SHALL support 1, 2, and 3 node clusters as first-class modes.

4.1 Voting Topology Rules

Total Nodes	Voters	Learners	Description
1	1	0	Single-node authoritative leader
2	2	0	Leader + follower
3	3	0	Standard Raft quorum
4+	3	N-3	Fixed quorum with learners

Requirements
	•	The system MUST maintain at most 3 voting members per consensus group.
	•	Additional nodes MUST join as learners.
	•	Voting membership MUST be explicitly managed.

⸻

5. Node Roles

Each node SHALL be in exactly one role:

5.1 Leader (Single Active)

Responsibilities:
	•	Accept all write requests
	•	Coordinate log replication
	•	Issue leases
	•	Drive membership transitions

Invariant:
	•	At most ONE leader SHALL exist at any time.

⸻

5.2 Follower (Voting Replica)

Responsibilities:
	•	Replicate committed state
	•	Participate in elections
	•	Serve read requests

⸻

5.3 Learner (Non-Voting Replica)

Responsibilities:
	•	Replicate committed state
	•	Serve read-only traffic
	•	Warm standby for promotion

Restrictions:
	•	Learners MUST NOT vote
	•	Learners MUST NOT become leader directly

⸻

6. Consensus Behavior by Cluster Size

6.1 Single Node Cluster (1 Node)

Behavior:
	•	Node SHALL self-elect as leader.
	•	Writes SHALL commit immediately.
	•	Reads SHALL always be linearizable.

Failure Mode:
	•	Node failure results in full system unavailability.

⸻

6.2 Two Node Cluster (2 Nodes)

Behavior:
	•	One node SHALL act as leader.
	•	Writes require replication to follower to commit.

Failure Mode:

Failure	Result
Leader lost	Cluster unavailable (no quorum)
Follower lost	Leader continues serving reads and writes

Safety Rule:
	•	No node SHALL self-elect without majority connectivity.

⸻

6.3 Three Node Cluster (3 Nodes)

Standard Raft semantics apply.

Failure Mode:

Failure	Result
One node lost	Cluster continues normally
Two nodes lost	Cluster becomes read-only


⸻

6.4 Four+ Node Cluster

Behavior:
	•	Only 3 nodes SHALL be voters.
	•	Remaining nodes operate as learners.

Rationale:
	•	Minimizes coordination overhead
	•	Preserves predictable quorum performance
	•	Simplifies membership transitions

⸻

7. Consistency Model

7.1 Write Semantics

All writes MUST:
	•	Be routed to leader
	•	Be replicated to quorum
	•	Be acknowledged only after commit

⸻

7.2 Read Modes

The system SHALL expose two read behaviors:

Linearizable Read

Guarantees:
	•	Reflects latest committed state
	•	Requires leader confirmation

Used for:
	•	Coordination primitives
	•	Security decisions
	•	Cluster state management

⸻

Stale-Allowed Read

Guarantees:
	•	Returns locally cached committed data
	•	May lag behind leader

Used for:
	•	UI display
	•	Status dashboards
	•	Non-critical consumers

⸻

8. Data Model

8.1 Key Space

Keys MUST be hierarchical:

/org/{orgId}/cluster/{clusterId}/resource/...


⸻

8.2 Revision Model

Each committed write MUST:
	•	Increment a global monotonic revision counter
	•	Produce a durable version identifier

⸻

8.3 Value Constraints

The system MUST enforce:
	•	Maximum value size limits
	•	Maximum total dataset budget
	•	Rejection of oversized writes

⸻

9. Coordination Primitives

These are first-class system features.

⸻

9.1 Lease

Definition:

A time-bounded ownership token.

Properties:
	•	Has TTL
	•	Must be periodically renewed
	•	Automatically expires on failure

Uses:
	•	Leadership assertions
	•	Scheduler ownership
	•	Ephemeral configuration

⸻

9.2 Lock

Definition:

A mutual exclusion primitive backed by leases.

Guarantees:
	•	Only one active holder at a time
	•	Automatic release on lease expiration

⸻

9.3 Election

Definition:

Leader selection for application-level roles.

Properties:
	•	Lease-backed
	•	Observable via watch streams

⸻

10. Watch System

The system MUST provide ordered change subscriptions.

Properties:
	•	Ordered by revision
	•	Resume-able from last seen revision
	•	Prefix-filtered

Guarantees:
	•	No reordering
	•	No missing committed updates

⸻

11. Membership Management

Membership transitions are high-risk operations and MUST be controlled.

⸻

11.1 Maintenance Mode

The system SHALL expose a cluster-wide maintenance mode.

While enabled:
	•	Membership changes are allowed
	•	Normal application writes MAY be paused or rate-limited

⸻

11.2 Join Procedure

New nodes MUST:
	1.	Join as learner
	2.	Fully replicate current state
	3.	Be explicitly promoted to voter

⸻

11.3 Promotion Rules

Promotion MUST require:
	•	Fully synchronized log state
	•	Operator or system authorization
	•	No concurrent membership changes

⸻

11.4 Removal Rules

Removal MUST ensure:
	•	Remaining voters satisfy quorum rules
	•	Cluster does not drop below configured minimum safety thresholds unless explicitly overridden

⸻

12. Partition Behavior

12.1 Loss of Quorum

When quorum is lost:
	•	Writes MUST be rejected
	•	Reads MAY be served as stale
	•	Lease renewals MUST fail

⸻

12.2 Minority Partition

Nodes in minority partitions MUST:
	•	Step down leadership
	•	Reject writes
	•	Continue serving stale reads

⸻

13. Failure Recovery

13.1 Restart Behavior

After restart:
	•	Node MUST recover last committed state
	•	MUST NOT assume leadership without quorum election

⸻

13.2 Snapshot Recovery

System MUST support:
	•	Full state snapshot export
	•	Restore into new cluster
	•	Deterministic recovery of revision history

⸻

14. Security Boundaries

14.1 Authorization

All write operations MUST:
	•	Be scoped by org and cluster ID
	•	Pass policy validation

⸻

14.2 Isolation

Clusters MUST NOT share state namespaces.

⸻

15. Operational Requirements

The system MUST expose:
	•	Current leader identity
	•	Quorum health status
	•	Commit revision number
	•	Membership roles
	•	Read-only vs writable state

⸻

16. Integration Contract (Underleaf)

The Raft store SHALL present:
	•	KV API
	•	Coordination API
	•	Watch API
	•	Admin API

Underleaf services MUST treat this store as:
	•	Source of truth for cluster intent
	•	Coordination backbone
	•	Event source for reactive materialization

⸻

17. Invariants Summary (Non-Negotiable)

The following MUST ALWAYS hold:
	•	Single leader at any time
	•	Writes only acknowledged after quorum commit
	•	Minority partitions cannot accept writes
	•	Membership changes serialized
	•	Leases expire without quorum
	•	Watch streams preserve commit order
