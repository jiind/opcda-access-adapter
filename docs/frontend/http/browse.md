# Browse

[Frontend overview](README.md) · [Documentation](../../README.md)

```text
POST /v1/browse
Content-Type: application/json
```

```json
{
  "path": ["Channel1", "Device1"],
  "filter": "all"
}
```

`path: []` means root. The path is a navigation sequence, not an ItemID and
is never used to infer one. `filter` is `all`, `branch`, or `item` and defaults
to `all`.

The runtime resets a hierarchical server to root and walks each source browse
name serially on the DA thread. Flat namespaces accept only the root path.
Item entries use the exact ItemID returned by `GetItemID`. A branch entry
carries one too when the source names it — A.3.1.2 has a wrapper obtain a
branch's ItemID from the same call — and carries none when the source declines.
Nothing is invented either way: an ItemID is never reconstructed from a browse
path. Canonical type and access rights are omitted when this
Browse interface does not supply them.

A recognized COM/RPC disconnect during a branch's `GetItemID` call fails the
request with HTTP 503 `DA_METHOD_FAILED` and the source HRESULT. The runtime
invalidates the connection and schedules a reconnect.

`IOPCBrowseServerAddressSpace` is optional. A server returning
`E_NOINTERFACE` reports Browse as `unsupported`, while known-ItemID Read stays
available. Exceeding the hard entry limit fails with
`BROWSE_RESULT_LIMIT_EXCEEDED`; results are never silently truncated.
