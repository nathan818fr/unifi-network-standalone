#!/usr/bin/env python3
"""Makes sample-console.unifi from the public sample backup: a console backup as UniFi OS writes one.

Only Network's folder is real: the file exists to test unifi-export, and holds no real data.
usage: make-console-backup.py <sample.unf> <output.unifi>
"""

import hashlib
import io
import struct
import subprocess
import sys
import tarfile
import zlib

UNF_KEY = b"bcyangkmluohmars".hex()
UNF_IV = b"ubntenterpriseap".hex()


def main(source, output):
    with open(source, "rb") as file:
        plain = openssl("-d", "-aes-128-cbc", "-nopad", "-K", UNF_KEY, "-iv", UNF_IV, input_data=file.read())

    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w:gz") as tar:
        for name, content in unf_entries(plain):
            add_entry(tar, f"backup/network/{name}", content)
        add_entry(tar, "backup/metadata.json", b'{"id":"fixture"}')
        add_entry(tar, "backup/ucore/config/unifi-core.key", b"not a real key, must stay out of the result")

    # A fixed IV, so that the fixture is the same each time. UniFi OS uses a random one.
    iv = hashlib.md5(buffer.getvalue()).digest()
    key = hashlib.sha256(b"ubiquiti").hexdigest()
    encrypted = openssl("-e", "-aes-256-cbc", "-K", key, "-iv", iv.hex(), input_data=buffer.getvalue())
    with open(output, "wb") as file:
        file.write(iv + encrypted)


def unf_entries(plain):
    """Yields (name, content), reading local records: the zip's end is cut in such files."""
    position = 0
    while plain[position : position + 4] == b"PK\x03\x04":
        name_length, extra_length = struct.unpack("<HH", plain[position + 26 : position + 30])
        name = plain[position + 30 : position + 30 + name_length].decode()
        body = position + 30 + name_length + extra_length
        inflater = zlib.decompressobj(-15)
        content = inflater.decompress(plain[body:])
        yield name, content
        # The rest of the data starts after the deflate stream, then a 16-byte data descriptor
        position = len(plain) - len(inflater.unused_data) + 16


def add_entry(tar, name, content):
    info = tarfile.TarInfo(name)
    if name.endswith("/"):
        info.type = tarfile.DIRTYPE
        info.mode = 0o755
        tar.addfile(info)
        return
    info.size = len(content)
    tar.addfile(info, io.BytesIO(content))


def openssl(*args, input_data):
    return subprocess.run(["openssl", "enc", *args], input=input_data, capture_output=True, check=True).stdout


if __name__ == "__main__":
    main(*sys.argv[1:])
