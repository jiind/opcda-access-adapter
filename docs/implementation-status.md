# Implementation status

The scoped implementation includes HTTP, gRPC, OPC UA, and MCP. There is no
public release yet. The local Windows destructive review remains open. The
callback lifetime fix has passed Windows regression tests. Windows application
control blocked the fixed adapter's Graybox rerun on the laptop.

This page summarizes current coverage and release prerequisites.

## Completed scope

| Area | Current state | Reference |
|---|---|---|
| DA runtime | Local COM, Browse, device Read, typed Write, reconnect, limits, and cleanup | [Architecture](architecture.md) |
| HTTP/JSON v0 | Status, Browse, Read, Write, and item properties | [HTTP API](frontend/http/README.md) |
| gRPC | Typed unary operations and server-streaming Subscribe | [gRPC API](frontend/grpc/README.md) |
| DA Subscribe core | One DA group per subscription, non-blocking callbacks, coalescing, explicit invalidation | [Architecture](architecture.md#subscriptions) |
| OPC UA | Browse, Read, Write, properties, Subscriptions, and MonitoredItems; `SecurityPolicy None` | [UA overview](frontend/opcua/README.md) |
| MCP stdio | Nine DA tools, explicit selection, bounded stdio, group ownership, and EOF cleanup | [MCP API](frontend/mcp/README.md) |
| Setup | Registration-only detection, explicit configuration, foreground execution, LocalService for network frontends | [Setup](operations/setup.md) |
| Item properties | DA queries and UA Table A.1 mapping | [ADR-0018](design/adr/0018-da-item-properties.md) |
| SAFEARRAY | Shape-preserving Read/Write and frontend encodings; no real-source validation | [ADR-0019](design/adr/0019-safearray-representation.md) |
| Distribution tooling | Windows archives, checksums, version metadata, attestations; no published release | [Release procedure](operations/releasing.md) |

HTTP remains the default. MCP is inactive until selected explicitly. Write is
disabled by default. Each process serves one local DA source through one frontend.

HTTP and MCP Write require JSON strings for `VT_BSTR` and explicit lower
bounds for arrays. Array Write accepts the Read value object with a matching
declared VARTYPE. A recognized connection loss during branch ID lookup enters
the DA runtime's reconnect path.

## Validation achieved

| Evidence | What it establishes | Limit |
|---|---|---|
| OPC Foundation DA 2.05a fixture, 386 and amd64 | Scoped COM operations, failures, reconnect, subscriptions, frontends, and soak | One source-built fixture |
| Graybox Gray Simulator 1.7.9.701, x86 | HTTP and gRPC operations and recovery, OPC UA Write and subscriptions, MCP tools, guided setup and Service lifecycle | The earlier rapid MCP crash still needs a Graybox rerun with the fix. HTTP LocalService activation received COM access denied. [Executed results](validation/compatibility.md#graybox-four-frontend-validation-on-2026-11-28) |
| asyncua, open62541, OPC Foundation .NET | UA client behavior over a scripted DA source; all three gated in CI | No DA-side or conformance proof |
| MCP stdio | Independent Python client against the real fixture on both architectures | Recorded scenarios, not every host or vendor |
| Windows unit and ABI tests | Both architectures, memory layouts, VARIANT/BSTR/SAFEARRAY handling | Unit coverage is not source interoperability |

[Compatibility evidence](validation/compatibility.md) records exact versions, commits,
run links, resource deltas, and scenario limits. It remains the authority for
executed compatibility claims.

## Release blocker and next work

1. Create the isolated local Windows environment.
2. Complete the [destructive review](validation/local-vm-destructive.md): COM
   launch/access/RunAs failures, load, resource bounds, reboot, both architectures,
   and cleanup. GitHub-hosted results do not satisfy this gate.
3. Record evidence for the revision proposed for release, then complete the
   [release checklist](operations/releasing.md#publishing).

Further compatibility work includes real DA SAFEARRAY values, more authorized
vendor installations, sources without optional DA interfaces, and vendor-specific
disconnect HRESULTs. UA Expert remains untested. Signed/encrypted UA policies,
new source protocols, storage, and a plugin framework are outside the current
scope, not unfinished implementation phases.

## Operational limits

- A permanently blocked COM call cannot be recovered by killing its owning
  thread. The runtime reports degraded state and requires process restart.
- A vendor-specific HRESULT is not guessed to mean disconnect. Add one only
  after observing it and updating the reconnect decision.
- Service access depends on the source accepting LocalService and scoped COM
  permissions. Setup does not grant permissions or run the service as LocalSystem.
- UA is for local interoperability only. Its standard departures and information
  losses are listed in the [mapping reference](frontend/opcua/data-access.md).

## Development checks

The PR gate includes quality, specification, interoperability, release-package,
Windows build and test, and OPC Foundation DA fixture checks. Check the
applicable results for a candidate with `gh pr checks`.
