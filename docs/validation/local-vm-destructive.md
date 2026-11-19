# Local Windows VM destructive review

## Execution status

**Not completed — no local-VM PASS result is recorded.** The isolated
environment must be created before executing the scenario matrix.

GitHub-hosted runner results do not satisfy this procedure. Evidence is valid
only when produced by the dedicated local KVM/libvirt VM named
`opcda-destructive-review` and recorded here with its base-image identity,
adapter commit, fixture commit, architecture, and resource observations.

## Isolation boundary

- Use a dedicated Windows Server evaluation VM on libvirt's NAT network.
- Do not start, modify, snapshot, or attach disks from unrelated local VMs.
- Keep the downloaded Microsoft base image immutable and run tests in a
  disposable copy-on-write disk.
- Create a clean snapshot after OS updates and build-tool installation.
- Build the pinned OPC Foundation fixture from source inside this VM, scan
  source and outputs with Defender, and never install a proprietary simulator.
- Store no real process values, production credentials, or vendor files in the
  VM.
- Restore all AppID values and COM registrations in `finally` cleanup, then
  revert or destroy the disposable test overlay.

## Required destructive matrix

| Area | Required observation |
|---|---|
| Normal | Connect, nested Browse, ordered partial Read, disabled and typed Write |
| Invalid input | malformed/truncated/oversized JSON, invalid UTF-8, NUL, excessive depth and batch sizes |
| Intentional HTTP abuse | slow headers/bodies, connection exhaustion, rapid requests, concurrency above all configured bounds, recovery after load |
| Source failure | kill and unregister the DA server during Reads and Writes; no stale Good value and no Write replay |
| Adapter failure | force-kill and restart the adapter repeatedly; COM registrations and source process remain bounded |
| Reboot | reboot the VM with registered fixture state, then confirm deterministic startup/cleanup and no persisted process value |
| DCOM/COM permissions | deny local Launch/Activation, observe exact HRESULT, restore the application ACL, and recover without weakening machine defaults |
| Identity | exercise an allowed standard local account and a denied account where the fixture supports it; never grant remote rights |
| Architecture | validate separate x86/386 and x64/amd64 registry views, proxies, fixture, and adapter |
| Resource bounds | monitor handles, private bytes, process count, queue depth, reconnect count, and event-log errors under soak |
| Security | Defender scans, no unexpected listener, default loopback isolation, no process values in adapter output |

The permission scenarios must snapshot the exact AppID values before mutation
and restore absence as absence, not as a broad replacement ACL. A test failure
must still execute cleanup. Machine-wide COM defaults and the unrelated Ubuntu
VM are never test targets.

The permission-mutation path requires both `-Destructive` and the literal
confirmation `-DestructiveConfirmation DISPOSABLE_VM_ONLY`. It must never be
run on a shared or production Windows host.

## Evidence record

The completed record must include:

- Microsoft evaluation image URL, file size, and SHA-256 observed locally;
- Windows build and installed update level;
- VM definition, network type, snapshot name, and test time window;
- adapter commit and both executable hashes;
- pinned fixture commit and source/output Defender results;
- exact scenario counts and PASS/FAIL/BLOCKED per architecture;
- bounded resource deltas and relevant DCOM event IDs;
- cleanup and snapshot-revert result.

Do not promote a release solely from a workflow result. A failing or incomplete
row remains explicit and blocks the local destructive-validation gate.
