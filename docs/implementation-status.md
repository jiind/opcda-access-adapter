# Implementation status

The scoped implementation includes HTTP, gRPC, OPC UA, and MCP. There is no
public release yet. The callback lifetime fix passed Windows regression tests
and 200 rapid subscription cycles against Graybox. The local Windows
destructive review remains open.

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
| SAFEARRAY | Shape-preserving Read/Write and frontend encodings, with fixture coverage for zero-based one-dimensional `VT_I4` and `VT_BSTR` on 386 and amd64 | [Fixture result](validation/compatibility.md#recorded-result-safearray-round-trips) |
| Distribution tooling | Windows archives, checksums, version metadata, attestations; no published release | [Release procedure](operations/releasing.md) |

HTTP remains the default. MCP is inactive until selected explicitly. Write is
disabled by default. Each process serves one local DA source through one frontend.

Guided setup prints the selected service endpoint and the saved MCP launch
settings. Foreground network runs and Windows services report listener startup
and DA connection changes separately. Windows services retry pending startup
and connection records when the Event Log write fails.

Configuration loading rejects duplicate field aliases and malformed Unicode.
UTF-8 files with a byte-order mark are accepted.

HTTP and MCP Write require JSON strings for `VT_BSTR` and explicit lower
bounds for arrays. Array Write accepts the Read value object with a matching
declared VARTYPE. Array dimensions must fit signed 32-bit indices. A
recognized connection loss during branch ID lookup enters
the DA runtime's reconnect path.

OPC UA nodes report the element `DataType` and array `ValueRank` once the
source provides the canonical VARTYPE. Array Write remains unsupported.
OPC UA scalar Write preserves the known canonical DA VARTYPE across Browse
and refreshes it when a later Read or notification reports a change.

## Validation achieved

| Evidence | What it establishes | Limit |
|---|---|---|
| OPC Foundation DA 2.05a fixture, 386 and amd64 | Scoped COM operations, failures, reconnect, subscriptions, frontends, and soak | One source-built fixture |
| Guided Windows Service | HTTP, gRPC, and OPC UA service-mode DA access, separate startup records, and cleanup on both architectures | LocalService with the pinned fixture. [Executed results](validation/compatibility.md#guided-setup-and-windows-service-result) |
| Graybox Gray Simulator 1.7.9.701, x86 | HTTP and gRPC operations and recovery, OPC UA Write and subscriptions, MCP tools and repeated group cleanup, guided setup and Service lifecycle | HTTP LocalService activation received COM access denied. [Executed results](validation/compatibility.md#graybox-four-frontend-validation-on-2026-11-28) |
| asyncua, open62541, OPC Foundation .NET | UA client behavior over a scripted DA source; all three gated in CI | No DA-side or conformance proof |
| MCP stdio | Independent Python client on both architectures, including same-session recovery, explicit resubscription, and group release | Pinned fixture and one stdio client. [MCP results](validation/compatibility.md#recorded-result-mcp-stdio-and-opc-foundation-fixture) |
| OPC UA shared ItemID | Two client handles receive initial, changed, and restored values from the same fixture item on both architectures. A repeated handle is refused in the same request | One writable scalar fixture item. [Subscription result](validation/compatibility.md#opc-ua-subscription-result) |
| SAFEARRAY notifications | gRPC and MCP carry changed and restored Int32 and BSTR arrays. OPC UA Publish carries changed and restored Int32 arrays. Both architectures passed | Zero-based one-dimensional fixture data. [Array results](validation/compatibility.md#recorded-result-safearray-round-trips) |
| Windows unit and ABI tests | Both architectures, memory layouts, VARIANT/BSTR/SAFEARRAY handling | Unit coverage is not source interoperability |

[Compatibility evidence](validation/compatibility.md) records exact versions, commits,
run links, resource deltas, and scenario limits. It remains the authority for
executed compatibility claims.

The [current Windows execution record](validation/compatibility.md#current-windows-execution-record)
covers the same adapter revision on the Graybox laptop and the 386 and amd64
OPC Foundation fixture jobs. The instrumented Graybox run reproduced an MCP
probe assertion when the source began returning different Quality, timestamp
and HRESULT metadata through both MCP and HTTP. The corrected comparison
passed all nine MCP tools in both Write modes during a full laptop rerun.
The rerun also exposed a mixed Device Read that refused valid items before
the registration cache filled. [Registration limit result](validation/compatibility.md#registration-limit-with-a-mixed-device-read)

The real-DA fixture's guided service checks verify both startup records after
the Application Event Log record ID captured before installation.

The Windows laptop reran the previously blocked builds and verified all four
Graybox foreground frontends. Temporary services passed start, stop and
restart. Graybox HTTP service activation under LocalService returned access
denied. [Laptop result](validation/compatibility.md#follow-up-rerun-with-smart-app-control-temporarily-disabled)

OPC UA Publish keeps one unsent notification per monitored item across response
limits. Deleting an item clears its pending notification. A DA disconnection
replaces unsent values with `Bad_NotConnected`. The fixture passed with a
one-notification Publish limit on both architectures.
[Subscription result](validation/compatibility.md#opc-ua-subscription-result)

The DA thread retains ownership of a new subscription until the caller
receives it. A request that ends before delivery leaves the group with the
DA thread for invalidation and release. The fixture passed 24 deadline trials
per architecture with zero active groups after each trial.
[Subscribe result](validation/compatibility.md#phase-7-da-subscribe-core-result)

OPC UA Publish chooses among ready subscriptions by priority and rotates
those of equal priority. Each subscription uses its own revised publishing
interval to determine when a keep-alive is due. Both fixture architectures
delivered lower priority data while a higher priority subscription was idle.
[Subscription result](validation/compatibility.md#opc-ua-subscription-result)

An empty OPC UA subscription accepts a new DA group deadband after a failed
first create or deletion of its final monitored item. Both cases have
regression coverage.

DA group updates are bound to the existing group's connection generation.
After source loss, OPC UA item deletion leaves the subscription awaiting an
explicit create request. Explicit creation discards pending values from the
invalidated connection. The DA fixture rejected a replacement from the old
generation on both architectures before an explicit Subscribe delivered again.
[Subscribe result](validation/compatibility.md#phase-7-da-subscribe-core-result)

OPC UA monitored item creation reports DA registration failures in request
order. Read denials are delivered through Publish. A candidate group that
would lose a live readable item is released and the current group continues
to deliver. Both fixture architectures rejected an unknown ItemID and
published the valid item in the same request.
[Subscription result](validation/compatibility.md#opc-ua-subscription-result)

An OPC UA item deletion that fails to replace a live DA group preserves the
current monitors and pending notifications. The client receives the failure
and can retry the deletion. Deleting a monitor with no change to active DA
registrations reuses the current group, including when the group limit is
reached. These paths have regression coverage.

The Windows fixture passed a DA outage within one OPC UA session on 386 and
amd64. Read and Publish carried no stale values. The source returned a live
value after reconnect, the refused Write was absent, and notifications resumed
after explicit monitor creation.
[Recovery result](validation/compatibility.md#opc-ua-recovery-within-one-session)

The real-DA gRPC probe passed on Windows 386 and amd64. Subscribe delivered
the exact changed and restored scalar values. During an outage, the same
client observed Read and Write refusal and stream invalidation. Explicit
resubscription after recovery delivered the current value, and cancellation
released the group.
[Stream recovery result](validation/compatibility.md#grpc-recovery-within-one-client-connection)

OPC UA clamps publishing intervals before converting them to durations.
Large positive requests use the configured maximum. The expiry check uses
the full UInt32 lifetime count without prematurely releasing the DA group.
Both cases have regression coverage.

OPC UA Browse applies the configured page limit across the UInt32 request
range. CreateSession uses its configured timeout bounds and stores the
negotiated duration for idle expiry. Regression tests cover oversized
requests, positive fractional timeout bounds, and expiry at the negotiated
boundary.

OPC UA property discovery refreshes when Read learns a new canonical VARTYPE.
The DA core, gRPC, and OPC UA require percent deadbands in the numeric range
from 0 through 100. The staged analog fixture passed on Windows 386 and amd64
with EURange discovery, filter refusal, source notification suppression and
delivery, and restoration.
[Analog fixture result](validation/compatibility.md#opc-ua-analog-properties-and-percent-deadband)

## Release blocker and next work

1. Create the isolated local Windows environment.
2. Complete the [destructive review](validation/local-vm-destructive.md): COM
   launch/access/RunAs failures, load, resource bounds, reboot, both architectures,
   and cleanup. GitHub-hosted results do not satisfy this gate.
3. Record evidence for the revision proposed for release, then complete the
   [release checklist](operations/releasing.md#publishing).

Further compatibility work includes real SAFEARRAYs with multiple dimensions
or nonzero lower bounds, more authorized vendor installations, sources without
optional DA interfaces, and vendor-specific disconnect HRESULTs. UA Expert
remains untested. Signed/encrypted UA policies,
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
