Below is a semantic RFC-style architecture document defining:
	•	(A) The Underleaf Agent Kernel (UA-K)
	•	(B) The new UA-managed userland components (MMA-style subsystems)

This is intentionally written like an internal platform RFC you can evolve into an official spec.

⸻

RFC-UA-K-001

Underleaf Agent Kernel & UA-Managed Component Architecture

Status: Draft
Audience: Underleaf Core Engineering
Last Updated: 2026-02
Scope: Edge data-plane architecture

⸻

1. Purpose

This RFC defines the kernelization of the Underleaf Agent (UA).

It establishes:
	1.	The UA Kernel: minimal trusted core
	2.	The UA-Managed Components model: all non-kernel capabilities
	3.	Strict responsibility boundaries
	4.	The syscall surface exposed by UA
	5.	Migration guidance from current UA implementation

The goal is to ensure that Underleaf evolves into a stable edge runtime substrate rather than an ever-growing daemon.

⸻

2. Architectural Philosophy

UA is treated as a node-local operating system kernel.

It must:
	•	change slowly
	•	remain minimal
	•	be security-critical
	•	expose stable primitives

Everything else runs in userland as UA-managed components.

⸻

3. Definitions

UA Kernel (UA-K)

The minimal trusted core running on every node.

Owns:
	•	identity
	•	execution authority
	•	cluster truth
	•	secrets
	•	provider lifecycle

UA-Managed Component (UMC)

A subsystem installed and supervised by UA that implements higher-level functionality.

Examples:
	•	Mycelium Mesh Agent (MMA)
	•	Deployment engine
	•	Cron scheduler
	•	Workflow engine
	•	Capability providers

Syscall

A structured request from a UMC to UA-K requesting a privileged action.

⸻

4. High-Level Topology

                CLOUD CONTROL PLANE
                        |
                     Event Bus
                        |
                   ┌──────────┐
                   │  UA-K    │  ← Kernel
                   └──────────┘
                        |
        ┌───────────────┼────────────────┐
        │               │                │
      MMA          Deploy Service     Providers
    (runtime)        (UMC)            (UMC)

UA-K = trusted kernel
UMCs = userland

⸻

5. UA Kernel Responsibilities

UA-K must only contain logic that requires root authority over the node.

If it does not require root trust, it MUST NOT be in UA-K.

⸻

5.1 Identity & Trust Root

UA-K is the cryptographic identity anchor.

Owns:
	•	device private keys
	•	org identity
	•	certificate issuance
	•	request signing
	•	trust verification

Provides syscalls:

GetNodeIdentity()
SignPayload()
VerifyTrust()
IssueLocalCertificate()


⸻

5.2 Secure Cloud Connection

UA-K is the only component allowed to communicate with cloud control plane.

Responsibilities:
	•	outbound connection to Event Bus
	•	inbound command verification
	•	message authenticity checks
	•	routing events to local subscribers

No UMC may connect to cloud directly.

⸻

5.3 Execution Authority

UA-K is the final authority over process and container execution.

Responsibilities:
	•	start/stop processes
	•	run containers
	•	resource allocation
	•	filesystem mounts
	•	namespace isolation

Syscalls:

RunProcess()
StopProcess()
InspectProcess()
AllocateResources()

UMCs request execution.
UA decides whether to allow.

⸻

5.4 Provider Lifecycle Authority

UA-K controls all provider installation and execution.

Responsibilities:
	•	install provider artifacts
	•	verify trust tier
	•	start providers
	•	revoke providers
	•	grant capabilities

Syscalls:

InstallProvider()
StartProvider()
StopProvider()
GrantCapability()
RevokeCapability()

MMA resolves routing but UA enforces permission.

⸻

5.5 Secret Custody

UA-K owns secrets.

Responsibilities:
	•	secret storage
	•	encryption
	•	TPM integration
	•	secret distribution

Syscalls:

StoreSecret()
GetSecret()
MountSecret()

No component accesses secrets directly.

⸻

5.6 Cluster Authority

UA-K owns cluster truth.

Responsibilities:
	•	Raft membership
	•	node roles
	•	cluster KV
	•	leader election

Syscalls:

GetClusterState()
ProposeClusterConfig()
JoinCluster()


⸻

5.7 Event Routing

UA-K routes events locally.

Responsibilities:
	•	receive events from cloud
	•	publish local events
	•	deliver to UMC subscribers

Syscalls:

EmitEvent()
SubscribeLocal()

UA-K does not interpret business meaning of events.

⸻

6. UA Kernel Non-Responsibilities

The following MUST NOT exist in UA-K:
	•	deployment compilation logic
	•	cron scheduling
	•	workflow orchestration
	•	capability routing decisions
	•	locality ranking
	•	business-specific features
	•	AI/voice logic
	•	app lifecycle orchestration

These must be implemented as UA-Managed Components.

⸻

7. UA-Managed Components (UMCs)

UMCs are userland subsystems supervised by UA-K.

They:
	•	run as providers or system services
	•	use UA syscalls
	•	may subscribe to events
	•	may request execution
	•	may manage their own state

They cannot bypass UA authority.

⸻

7.1 Core UMC Categories

Runtime Components
	•	MMA (service mesh runtime)
	•	provider supervisors

System Components
	•	deployment engine
	•	cron engine
	•	workflow engine
	•	metrics collectors

Capability Providers
	•	AI runtime
	•	robotics
	•	backups
	•	integrations

⸻

8. UMC Lifecycle

UMCs are installed as providers.

Lifecycle:

install → verify trust → start → supervised → stop → remove

UA-K supervises:
	•	restart policy
	•	resource limits
	•	health checks

⸻

9. UMC ↔ UA Communication Model

UMCs communicate with UA via local IPC.

Recommended transport:
	•	Unix domain socket
	•	gRPC or JSON-RPC

Pattern:

UMC → syscall → UA
UA → response → UMC

UMCs may subscribe to:
	•	cluster updates
	•	provider changes
	•	events

⸻

10. Mycelium Mesh Agent (MMA) as UMC

MMA becomes a UA-Managed Component.

Responsibilities:
	•	capability binding resolution
	•	service discovery
	•	locality routing
	•	policy evaluation
	•	telemetry

MMA must not:
	•	install providers
	•	execute containers
	•	hold secrets
	•	override UA decisions

MMA requests:

GrantCapability()
ResolveProvider()

UA enforces.

⸻

11. Deployment Engine as UMC

Current UA deployment logic must be extracted.

New deployment service:

Responsibilities:
	•	diff desired state
	•	compile deployment plan
	•	request execution via UA
	•	monitor progress

UA only executes.

⸻

12. Cron / Workflow Engine as UMC

Scheduling must be userland.

Responsibilities:
	•	schedule tasks
	•	emit events
	•	request execution

UA provides:

RunProcess()
EmitEvent()


⸻

13. Provider Model

Providers are a special type of UMC.

They implement capabilities.

UA controls:
	•	installation
	•	execution
	•	permissions

MMA routes to them.

⸻

14. Security Model

UA-K is the only trusted authority.

UMCs are sandboxed.

UMCs cannot:
	•	access secrets without syscall
	•	run processes without syscall
	•	connect to cloud directly

All privileged actions require UA.

⸻

15. Kernel Invariants

These are non-negotiable:
	1.	UA-K never contains business logic
	2.	UA-K exposes minimal stable API
	3.	All node authority flows through UA
	4.	UMCs cannot bypass UA
	5.	UA must be replaceable without rewriting UMCs

⸻

16. Migration Plan

Current UA contains:
	•	deployment logic
	•	scheduling logic
	•	provider orchestration

These must be extracted into UMCs.

Phases:

Phase 1

Define syscall surface
Stabilize UA-K API

Phase 2

Extract deployment engine

Phase 3

Extract scheduling

Phase 4

Extract mesh routing (MMA fully userland)

⸻

17. Benefits

Kernelization enables:
	•	stable core
	•	modular features
	•	offline operation
	•	multi-mesh support
	•	future runtimes
	•	safer upgrades

⸻

18. Open Questions
	•	Do UMCs run as containers or binaries?
	•	How strict is sandboxing?
	•	Should UA expose WASM runtime?
	•	How are syscall permissions scoped?

⸻

19. Summary

UA must evolve into:

A minimal, stable, trusted kernel for the edge node.

Everything else becomes:

UA-Managed Components running in userland.

This separation is critical to ensure that Underleaf scales as a platform rather than collapsing into a monolithic daemon.
