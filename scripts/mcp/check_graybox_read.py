#!/usr/bin/env python3
"""Compare Device Read metadata through MCP and HTTP on a selected Graybox source."""

import argparse
from contextlib import contextmanager
import json
from pathlib import Path
import socket
import subprocess
import tempfile
import time
from urllib.error import URLError
from urllib.request import ProxyHandler, Request, build_opener
from uuid import UUID

from check import Client, wait_for_status


ITEM_IDS = ("enum.color", "__opcda_graybox_metadata_missing__")
READ_ITEMS = [{"itemId": item_id} for item_id in ITEM_IDS]
METADATA_FIELDS = (
    "ok", "dataType", "canonicalDataType", "quality", "timestampPresent",
    "hresult", "accessRights", "errorCode",
)


def write_config(path, clsid, frontend):
    config = {"version": 4, "source": {"clsid": clsid}, "frontend": frontend, "writeEnabled": False}
    path.write_text(json.dumps(config), encoding="utf-8")


@contextmanager
def http_reference(executable, directory, clsid):
    with socket.socket() as socket_probe:
        socket_probe.bind(("127.0.0.1", 0))
        address = f"127.0.0.1:{socket_probe.getsockname()[1]}"
    config = directory / "http-reference.json"
    write_config(config, clsid, {"type": "http", "httpListen": address})
    opener = build_opener(ProxyHandler({}))

    def request(path, body=None):
        headers = {"Content-Type": "application/json"} if body is not None else {}
        with opener.open(Request(f"http://{address}{path}", data=body, headers=headers), timeout=2) as response:
            return json.load(response)

    process = subprocess.Popen(
        [str(executable), "run", "--config", str(config)],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    try:
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            assert process.poll() is None, "HTTP reference exited during startup"
            try:
                status = request("/v1/status")
            except (URLError, TimeoutError):
                status = {}
            if status.get("state") == "connected":
                assert status["source"]["clsid"].lower() == clsid.lower(), "HTTP reference selected another source"
                break
            time.sleep(0.1)
        else:
            raise AssertionError("HTTP reference did not connect")

        body = json.dumps({"source": "device", "items": READ_ITEMS}).encode("utf-8")
        yield lambda: request("/v1/read", body)["results"]
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)


def read_metadata(rows):
    assert tuple(row["itemId"] for row in rows) == ITEM_IDS, "Device Read lost ItemID order"
    good, missing = rows
    assert good["ok"] and good["hresult"]["value"] >= 0, "valid Graybox item failed Device Read"
    assert good["dataType"]["name"] == good["canonicalDataType"]["name"] == "VT_I4", "valid Graybox item type changed"
    assert type(good.get("value")) is int, "valid VT_I4 result has no integer value"
    assert type(good.get("quality")) is int and 0 <= good["quality"] <= 65535, "invalid raw DA Quality"
    assert not missing["ok"] and missing["hresult"]["hex"] == "0xC0040007", "unknown ItemID HRESULT changed"
    assert missing.get("value") is None and missing.get("quality") is None, "failed item carried a sample"
    for row in rows:
        present = row["timestampPresent"]
        assert type(present) is bool and present == (row["timestamp"] is not None), "inconsistent source timestamp presence"
    return [{key: row.get(key) for key in METADATA_FIELDS} for row in rows]


def compare_device_metadata(executable, directory, clsid, client):
    def mcp_read():
        return client.tool("da_read", {"items": READ_ITEMS})["results"]

    first = read_metadata(mcp_read())[0]
    print(
        f"GRAYBOX_MCP_FIRST_READ quality={first['quality']} "
        f"timestampPresent={first['timestampPresent']} hresult={first['hresult']['hex']}",
        flush=True,
    )
    with http_reference(executable, directory, clsid) as reference:
        for _ in range(3):
            before = read_metadata(reference())
            observed = read_metadata(mcp_read())
            after = read_metadata(reference())
            if before != after:
                continue
            for item_id, actual, expected in zip(ITEM_IDS, observed, before):
                differences = [key for key in METADATA_FIELDS if actual[key] != expected[key]]
                assert not differences, f"MCP and HTTP metadata differ for {item_id}: {', '.join(differences)}"
            sample = observed[0]
            print(
                f"GRAYBOX_MCP_METADATA_PASS quality={sample['quality']} "
                f"timestampPresent={sample['timestampPresent']} hresult={sample['hresult']['hex']} valuesLogged=false",
                flush=True,
            )
            return
    raise AssertionError("Graybox metadata changed during all three HTTP comparison windows")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--adapter", required=True, type=Path)
    parser.add_argument("--clsid", required=True, type=UUID)
    args = parser.parse_args()
    executable = args.adapter.resolve(strict=True)
    clsid = "{" + str(args.clsid) + "}"
    with tempfile.TemporaryDirectory(prefix="opcda-graybox-read-") as temporary:
        directory = Path(temporary)
        config = directory / "mcp.json"
        write_config(config, clsid, {"type": "mcp"})
        client = Client(executable, config)
        try:
            client.call("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                                       "clientInfo": {"name": "graybox-read-probe", "version": "1"}})
            client.send("notifications/initialized", {})
            status = wait_for_status(client, lambda item: item["runtimeState"] == "connected", "MCP did not connect")
            assert status["source"]["clsid"].lower() == clsid.lower(), "MCP selected another source"
            assert not status["writeEnabled"], "MCP Write must be disabled"
            compare_device_metadata(executable, directory, clsid, client)
        finally:
            client.close()


if __name__ == "__main__":
    main()
