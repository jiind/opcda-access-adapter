# Typed value Write

[Frontend overview](README.md) · [Documentation](../../README.md)

```text
POST /v1/write
Content-Type: application/json
```

```json
{
  "items": [
    {
      "itemId": "Random.Int2",
      "dataType": "VT_I2",
      "valueEncoding": "json",
      "value": 42
    }
  ]
}
```

Write is value-only and disabled by default. Set `OPCDA_WRITE_ENABLED=true`
only for an explicitly authorized source. While disabled, the endpoint returns
HTTP 403 with `WRITE_DISABLED` before admitting source work.

The supplied VARTYPE must exactly equal the canonical VARTYPE obtained during
item registration. The adapter performs no canonical-type conversion. The
v0 Write representations are:

| VARTYPE | Required representation |
|---|---|
| `VT_EMPTY`, `VT_NULL` | JSON `null` |
| `VT_I1/UI1/I2/UI2/I4/UI4/INT/UINT/ERROR` | in-range JSON integer |
| `VT_I8/UI8` | in-range decimal JSON string |
| finite `VT_R4/R8` | JSON number |
| non-finite `VT_R4/R8` | `"NaN"`, `"+Infinity"`, or `"-Infinity"` with `valueEncoding: float-special` |
| `VT_ARRAY` | an object carrying the shape and the elements, with `valueEncoding: array` |
| `VT_BOOL` | JSON boolean |
| `VT_BSTR` | bounded JSON string, including `""` for an empty string |

Other types are explicitly unsupported. The result order matches the request,
and per-item source HRESULTs remain in HTTP 200 responses. A mismatch with the
item's canonical type produces an item-level `TYPE_MISMATCH`. A method-level
HRESULT is a source-layer error.
The adapter makes one source Write call per admitted batch and never retries,
replays, or promises rollback. If the frontend deadline expires after COM has
started, the source outcome is unknown.

## Array values

An array travels as its shape beside its elements, under `valueEncoding: array`:

```json
{
  "valueEncoding": "array",
  "value": {
    "elementDataType": {"code": 3, "name": "VT_I4"},
    "dimensions": [
      {"lowerBound": 1, "length": 3},
      {"lowerBound": 0, "length": 2}
    ],
    "elements": [10, 11, 12, 13, 14, 15]
  }
}
```

`elements` is flat and ordered so the **last dimension varies fastest**, so a
client can rebuild the array from the description alone. The lower bound is the
one the source reported: it is never normalised to zero, because a client
writing back to an index it read must reach the element it read.

Elements follow the same per-type rules a scalar does — `VT_I8`/`VT_UI8` as
decimal strings, `VT_BSTR` as JSON strings. A non-finite float is the one
exception: a scalar names it through the sibling `valueEncoding` field, which an
element does not have, so inside an array it is spelled in place as `"NaN"`,
`"+Infinity"` or `"-Infinity"` where a number would go.

A Write can reuse the Read result's `value` object. In the Write item, set
`dataType` to the symbolic array type (`"VT_I4|VT_ARRAY"`) and `valueEncoding`
to `"array"`. The optional `elementDataType` accepts the Read object's
`code` and `name` or a symbolic name such as `"VT_I4"`. When supplied, the
type must agree with `dataType`. A mismatch returns HTTP 400 `TYPE_MISMATCH`.

Each dimension must specify its integer `lowerBound` (including zero) and
positive `length`. The calculated upper bound must fit in a signed 32-bit
index. An invalid bound, a shape that disagrees with `elements`, or a null
value or element declared as `VT_BSTR` returns HTTP 400 `INVALID_VALUE`
before the adapter calls the source.

See the [array shape design](../../design/adr/0019-safearray-representation.md)
and [executed fixture result](../../validation/compatibility.md#recorded-result-safearray-round-trips).
