RFC-UL-UA-002

Underleaf Agent Capability Resolution & Artifact Execution Protocol

Status: Draft
Version: 1.0
Audience: Runtime Platform, Product Integrations
Scope: Edge Agent Runtime

⸻

1. Purpose

The Underleaf Agent (UA) provides:
	•	Local capability resolution
	•	Provider installation and lifecycle
	•	Sandbox enforcement
	•	Artifact management
	•	Product-facing capability API

UA acts as the execution authority for Underleaf-managed systems.

⸻

2. Core Responsibilities

UA MUST:
	•	Maintain cached registry state
	•	Resolve providers deterministically
	•	Enforce trust and sandbox policies
	•	Abstract artifact source complexity
	•	Provide local-first APIs

UA MUST NOT:
	•	Expose raw registry to products
	•	Allow direct artifact installation by clients
	•	Skip signature verification

⸻

3. UA Local API (Product Interface)

⸻

3.1 Ensure Capability

POST /v1/capabilities/ensure

Used by Voice Assistant and other local services.

Example:

{
  "capability": {
    "id": "iot.hue.bridge",
    "version_range": "^1.0"
  },
  "constraints": {
    "trust": "official_only"
  }
}


⸻

3.2 Resolve Capability

GET /v1/capabilities/resolve?capability=iot.light.control

Returns active provider endpoint.

⸻

3.3 List Available Capabilities

GET /v1/capabilities/available


⸻

4. Registry Sync Behavior

UA MUST:
	•	Perform initial snapshot sync
	•	Periodically perform delta sync
	•	Cache full registry locally
	•	Support indefinite offline operation

⸻

5. Provider Resolution Algorithm

UA MUST:
	1.	Filter providers by capability
	2.	Enforce version compatibility
	3.	Filter by platform support
	4.	Apply trust constraints
	5.	Prefer highest trust tier
	6.	Prefer newest compatible version
	7.	Verify signatures
	8.	Install artifact
	9.	Launch provider with sandbox profile

⸻

6. Artifact Installation System

UA MUST support:

6.1 OCI Containers (Primary)
	•	Verify digest
	•	Verify signature
	•	Pull image
	•	Launch sandboxed container

⸻

6.2 Signed Binary Artifacts
	•	Validate signature
	•	Install into isolated directory
	•	Apply OS sandboxing

⸻

6.3 Git Repositories
	•	Clone
	•	Verify commit hash
	•	Build in isolated environment
	•	Cache compiled artifact

⸻

6.4 Package Managers (npm, pip)
	•	Create isolated environment
	•	Pin dependency versions
	•	Block untrusted scripts by default

⸻

7. Sandbox Enforcement

UA MUST implement profiles:

Profile	Permissions
strict	localhost only
standard	LAN + secrets
privileged	hardware + USB

Privileged providers MUST require explicit approval.

⸻

8. Resource Control

UA MUST enforce:
	•	CPU quotas
	•	memory caps
	•	restart limits
	•	disk quotas

⸻

9. Secrets Handling

UA MUST:
	•	store secrets encrypted locally
	•	inject secrets at runtime
	•	isolate secrets per provider

⸻

10. Offline Operation

UA MUST:
	•	allow existing providers to run offline
	•	resolve from cached registry
	•	block unknown new installs when offline
	•	allow sideload installs

⸻

11. Security Requirements

UA MUST:
	•	verify all signatures
	•	isolate provider runtimes
	•	maintain audit logs
	•	restrict cross-provider access
	•	rotate credentials

⸻

12. Extension Model

UA SHOULD support pluggable:
	•	artifact handlers
	•	sandbox adapters
	•	secret backends

⸻

Final Architectural Relationship

UCRS (Cloud Authority)
   ↓ signed registry sync
UA (Local Resolver + Runtime)
   ↓ capability API
Voice Assistant / MFG

This separation gives you:
	•	cloud governance
	•	local resilience
	•	product independence
	•	long-term platform leverage
