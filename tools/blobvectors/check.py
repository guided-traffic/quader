#!/usr/bin/env python3
"""Check the vectors of the stored format quader-blob-v1 with an implementation independent of Quader's Go code.

storage.md R-STOR-079: the builder checks every case of internal/blob/blobcrypt/testdata once, by hand and not in CI, with
Python's cryptography package, before the vectors are committed; the developer page records the date and the outcome.

This script implements the format from the specification's text (R-STOR-076, R-STOR-077, R-STOR-080's nonces, R-STOR-081's
tails, R-STOR-086's names): it rebuilds every positive case from its inputs and compares byte for byte, reads it back
through steps 1 to 7 of R-STOR-077, and reads every negative case expecting its reason. It also compares the specification's
known answers, copied from its text.

    python3 -I tools/blobvectors/check.py internal/blob/blobcrypt/testdata

Exit 0 when every case agrees, 1 otherwise. It prints case names and reasons, never a key.
"""

import base64
import hashlib
import hmac as std_hmac
import json
import os
import re
import sys

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.hkdf import HKDF

FMT = b"quader-blob-v1"
SEG = 65536
OVERHEAD = 28
FINAL = 68
MAX_PLAIN = 1 << 42


class Refused(Exception):
    """A read refused with the name of its step (R-STOR-077)."""

    def __init__(self, reason):
        super().__init__(reason)
        self.reason = reason


def u8(x):
    return bytes([x])


def u64(x):
    return x.to_bytes(8, "big")


def hkdf(ikm, salt, info):
    return HKDF(algorithm=hashes.SHA256(), length=32, salt=salt or None, info=info).derive(ikm)


def hmac_sha256(key, msg):
    return std_hmac.new(key, msg, hashlib.sha256).digest()


def segments(length):
    return (length + SEG - 1) // SEG


def stored_size(length):
    return length + OVERHEAD * segments(length) + FINAL


def plain_size(stored):
    """The inverse of S as R-STOR-076 states it; None for a size not in the format."""
    if stored < FINAL or stored > stored_size(MAX_PLAIN):
        return None
    d = stored - FINAL
    if d == 0:
        return 0
    n = -(-d // (SEG + OVERHEAD))
    r = d - (SEG + OVERHEAD) * (n - 1)
    if not 29 <= r <= SEG + OVERHEAD:
        return None
    return d - OVERHEAD * n


def b64d(value, size):
    """base64url without padding, in its one canonical spelling, of exactly size bytes; None otherwise."""
    if value is None or not re.fullmatch(r"[A-Za-z0-9_-]*", value):
        return None
    try:
        raw = base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))
    except ValueError:
        return None
    if len(raw) != size or base64.urlsafe_b64encode(raw).rstrip(b"=").decode() != value:
        return None
    return raw


def b64e(raw):
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode()


def plaintext(case):
    p = case["plaintext"]
    if p.get("rule"):
        assert p["rule"] == "i mod 251", p["rule"]
        return bytes(i % 251 for i in range(p["length"]))
    return bytes.fromhex(p["hex"])


# The writer: R-STOR-076 and R-STOR-080.


def nonce(seed, mark, *indexes):
    return hmac_sha256(seed, FMT + b" nonce" + u8(mark) + b"".join(u64(i) for i in indexes))[:12]


def seal_object(dek, seed, plain):
    kseg = AESGCM(hkdf(dek, b"", FMT + b" segments"))
    out = []
    n = segments(len(plain))
    for i in range(n):
        nc = nonce(seed, 0x00, i)
        out.append(nc + kseg.encrypt(nc, plain[SEG * i : SEG * (i + 1)], FMT + u8(0x00) + u64(i)))
    t = u64(len(plain)) + hashlib.sha256(plain).digest()
    nc = nonce(seed, 0x01, n)
    out.append(nc + kseg.encrypt(nc, t, FMT + u8(0x01) + u64(n)))
    return b"".join(out)


def seal_tail(dek, seed, offset, plain):
    ktail = AESGCM(hkdf(dek, b"", FMT + b" staging tails"))
    out = []
    n = segments(len(plain))
    for j in range(n):
        mark = 0x03 if j == n - 1 else 0x02
        nc = nonce(seed, mark, offset, j)
        out.append(nc + ktail.encrypt(nc, plain[SEG * j : SEG * (j + 1)], FMT + u8(mark) + u64(offset) + u64(j)))
    return b"".join(out)


def wrap(storage_key, key_id, salt, wrap_nonce, dek, digest, length):
    kw = AESGCM(hkdf(storage_key, salt, FMT + b" wrap"))
    ad = FMT + b" wrap" + u8(len(key_id)) + key_id.encode()
    w = salt + wrap_nonce + kw.encrypt(wrap_nonce, dek + digest + u64(length), ad)
    return {
        "quader-format": FMT.decode(),
        "quader-key": key_id,
        "quader-wrap": b64e(w),
        "quader-commitment": b64e(hkdf(dek, b"", FMT + b" commitment")),
    }


# The reader: steps 1 to 7 of R-STOR-077.


def read_object(meta, keys, copy, obj):
    if meta.get("quader-format") != FMT.decode():
        raise Refused("format")
    key_id = meta.get("quader-key")
    if key_id is None or not re.fullmatch(r"[a-z0-9-]{3,64}", key_id):
        raise Refused("record")
    if key_id not in keys:
        raise Refused("unknown_key")
    w = b64d(meta.get("quader-wrap"), 116)
    if w is None:
        raise Refused("record")
    kw = AESGCM(hkdf(keys[key_id], w[:16], FMT + b" wrap"))
    try:
        unwrapped = kw.decrypt(w[16:28], w[28:], FMT + b" wrap" + u8(len(key_id)) + key_id.encode())
    except InvalidTag:
        raise Refused("record") from None
    dek, digest, length = unwrapped[:32], unwrapped[32:64], int.from_bytes(unwrapped[64:72], "big")
    if length > MAX_PLAIN:
        raise Refused("record")
    if digest != bytes.fromhex(copy["digest"]) or length != copy["size"]:
        raise Refused("record")
    if stored_size(length) != copy["storedSize"] or stored_size(length) != len(obj):
        raise Refused("size")
    c = b64d(meta.get("quader-commitment"), 32)
    if c is None or not std_hmac.compare_digest(c, hkdf(dek, b"", FMT + b" commitment")):
        raise Refused("commitment")
    kseg = AESGCM(hkdf(dek, b"", FMT + b" segments"))
    n, pos, plain = segments(length), 0, []
    for i in range(n):
        size = min(SEG, length - SEG * i) + OVERHEAD
        seg, pos = obj[pos : pos + size], pos + size
        try:
            plain.append(kseg.decrypt(seg[:12], seg[12:], FMT + u8(0x00) + u64(i)))
        except InvalidTag:
            raise Refused("segment") from None
    fin = obj[pos : pos + FINAL]
    try:
        t = kseg.decrypt(fin[:12], fin[12:], FMT + u8(0x01) + u64(n))
    except InvalidTag:
        raise Refused("final") from None
    if int.from_bytes(t[:8], "big") != length or t[8:] != digest:
        raise Refused("final")
    out = b"".join(plain)
    if hashlib.sha256(out).digest() != digest:
        raise Refused("digest")
    return out


def read_tail(dek, offset, length, sha, obj):
    if len(obj) != length + OVERHEAD * segments(length):
        raise Refused("size")
    ktail = AESGCM(hkdf(dek, b"", FMT + b" staging tails"))
    n, pos, plain = segments(length), 0, []
    for j in range(n):
        size = min(SEG, length - SEG * j) + OVERHEAD
        seg, pos = obj[pos : pos + size], pos + size
        mark = 0x03 if j == n - 1 else 0x02
        try:
            plain.append(ktail.decrypt(seg[:12], seg[12:], FMT + u8(mark) + u64(offset) + u64(j)))
        except InvalidTag:
            raise Refused("segment") from None
    out = b"".join(plain)
    if hashlib.sha256(out).digest() != sha:
        raise Refused("digest")
    return out


def keyed_name(name_key, prefix, digest):
    m = hmac_sha256(name_key, FMT + b" name" + digest).hex()
    return (prefix + "/" if prefix else "") + "keyed/" + m[0:2] + "/" + m[2:4] + "/" + m


# The known answers, copied from storage.md R-STOR-076, R-STOR-077 and R-STOR-086.

KNOWN = {
    "segment key": "3671ce0541d176175bb26410c7f0fc2da1b39c6c2aac6f5e6370a6d8ecab751d",
    "commitment": "1f85b68c333456650f07544250d71ec2183829bbd73dbe32cead4ecbc7e47733",
    "tail key": "c1ad0bdb618d3d83b751d1369a6ad44e43b17af56a2f61426198c078b8c87af5",
    "empty": "0a34ba7650ec909a28eb0538f53f7a84be9ab01ed68575fe09db310b105f9920c9514c8d9c6697e73cd3ad5a6ffa194411d4ecc171d9eb47d7abee250da8fbad07dd2cad",
    "a": "05c628666f8f6834df05aa27f848cff1e0157ab14ec8c092f5c25a9c72182e548168cd912fdcca99e766a8b8075a85ce9f94df79a3778bef284bd3aa9bfabf3803858b1f077d62b7ffbc5fef76005697ce8f12da2adf7fb2b20f25458434a6de71",
    "rule 65537 stored sha256": "f547d3127b4c42a11f36ff45996bf301cd35d84bbde6c4d0c22b0beb850ff144",
    "wrap": "gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam7m4IyuNMsFf5oiyZvVVCx8bBxSnY5kNUOulc9ByS8wknq4HpO9Zz-lqXOkpDX_rlAAJ5DJCXzMRmCNWG9_7zOuut11EonL3axPC5fq0E9cwuDeQCQ1A6Xs",
    "record commitment": "H4W2jDM0VmUPB1RCUNcewhg4KbvXPb4yzq1Oy8fkdzM",
    "keyed name": "quader/keyed/99/42/9942d541941e3141ba35550ee20b7bbf8c60fb3acc8123325c1b293b0e2922a6",
}


def seq(start, n):
    return bytes((start + i) % 256 for i in range(n))


def check_known(problems):
    dek, seed, sk = seq(0x00, 32), seq(0x60, 32), seq(0x20, 32)
    got = {
        "segment key": hkdf(dek, b"", FMT + b" segments").hex(),
        "commitment": hkdf(dek, b"", FMT + b" commitment").hex(),
        "tail key": hkdf(dek, b"", FMT + b" staging tails").hex(),
        "empty": seal_object(dek, seed, b"").hex(),
        "a": seal_object(dek, seed, b"a").hex(),
        "rule 65537 stored sha256": hashlib.sha256(seal_object(dek, seed, bytes(i % 251 for i in range(65537)))).hexdigest(),
        "keyed name": keyed_name(seq(0x40, 32), "quader", hashlib.sha256(b"a").digest()),
    }
    rec = wrap(sk, "sk-default-1-0a1b2c3d", seq(0x80, 16), seq(0x90, 12), dek, hashlib.sha256(b"a").digest(), 1)
    got["wrap"], got["record commitment"] = rec["quader-wrap"], rec["quader-commitment"]
    for name, want in KNOWN.items():
        if got[name] != want:
            problems.append("known answer " + name + " differs")
    try:
        read_object(
            dict(rec, **{"quader-key": "sk-default-2-0a1b2c3d"}),
            {"sk-default-2-0a1b2c3d": sk},
            {"digest": hashlib.sha256(b"a").hexdigest(), "size": 1, "storedSize": 97},
            bytes.fromhex(KNOWN["a"]),
        )
        problems.append("the known wrap opened with the identifier sk-default-2-0a1b2c3d")
    except Refused as e:
        if e.reason != "record":
            problems.append("the known wrap under sk-default-2-0a1b2c3d gave " + e.reason)
    for size, stored in [(0, 68), (1, 97), (65535, 65631), (65536, 65632), (65537, 65661), (16 << 20, 16784452),
                         (100 << 20, 104902468), (10 << 30, 10742005828), (1 << 42, 4399925559364)]:
        if stored_size(size) != stored or plain_size(stored) != size:
            problems.append("the size table differs at %d" % size)
    for stored in [0, 67, 69, 96, 65633, 65660]:
        if plain_size(stored) is not None:
            problems.append("%d is in the format" % stored)


def check_case(case, base, problems):
    name = case["name"]

    def load(rel):
        with open(os.path.join(base, rel), "rb") as f:
            return f.read()

    def meta():
        return json.loads(load(case["record"]))

    keys = {k["id"]: bytes.fromhex(k["key"]) for k in case.get("readerKeys", [])}
    if case["kind"] == "name":
        got = keyed_name(bytes.fromhex(case["nameKey"]), case["prefix"], bytes.fromhex(case["digest"]))
        return "ok" if got == case["objectName"] else "the name differs"
    obj = load(case["object"])
    if hashlib.sha256(obj).hexdigest() != case["storedSha256"] or len(obj) != case["storedSize"]:
        return "the object file is not the one the vector names"
    plain = plaintext(case)
    if case["kind"] == "tail":
        dek, seed, offset = bytes.fromhex(case["dataKey"]), bytes.fromhex(case["seed"]), case["offset"]
        if seal_tail(dek, seed, offset, plain) != obj:
            return "the tail does not rebuild from its inputs"
        for j, nc in enumerate(case["segmentNonces"]):
            mark = 0x03 if j == len(case["segmentNonces"]) - 1 else 0x02
            if nonce(seed, mark, offset, j).hex() != nc or obj[j * (SEG + OVERHEAD) :][:12].hex() != nc:
                return "tail nonce %d differs" % j
        try:
            got = read_tail(dek, offset, len(plain), bytes.fromhex(case["plaintext"]["sha256"]), obj)
        except Refused as e:
            return "the tail is refused: " + e.reason
        return "ok" if got == plain else "the tail reads back other bytes"
    if case.get("reason"):
        try:
            read_object(meta(), keys, case["copy"], obj)
            return "the negative case was read"
        except Refused as e:
            return "ok" if e.reason == case["reason"] else "refused with %s, the vector says %s" % (e.reason, case["reason"])
    dek, seed = bytes.fromhex(case["dataKey"]), bytes.fromhex(case["seed"])
    if hashlib.sha256(plain).hexdigest() != case["plaintext"]["sha256"]:
        return "the plaintext's SHA-256 differs"
    if seal_object(dek, seed, plain) != obj:
        return "the object does not rebuild from its inputs"
    n = segments(len(plain))
    want_nonces = [nonce(seed, 0x00, i).hex() for i in range(n)] + [nonce(seed, 0x01, n).hex()]
    if case["segmentNonces"] != want_nonces:
        return "the explicit nonces are not the ones the seed derives"
    wk = case["wrapKey"]
    rec = wrap(bytes.fromhex(wk["key"]), wk["id"], bytes.fromhex(case["salt"]), bytes.fromhex(case["wrapNonce"]), dek,
               hashlib.sha256(plain).digest(), len(plain))
    if rec != meta():
        return "the record does not rebuild from its inputs"
    if sum(len(k) + len(v) for k, v in rec.items()) > 2048:
        return "the record is larger than 2 KiB"
    try:
        got = read_object(meta(), keys, case["copy"], obj)
    except Refused as e:
        return "the object is refused: " + e.reason
    if case["name"] == "older-a":
        return "ok" if got == b"a" else "the older object reads back other bytes"
    return "ok" if got == plain else "the object reads back other bytes"


def main(argv):
    if len(argv) != 2:
        print(__doc__.strip().splitlines()[-3].strip())
        return 2
    base = argv[1]
    with open(os.path.join(base, "vectors.json"), encoding="utf-8") as f:
        vectors = json.load(f)
    problems = []
    check_known(problems)
    reasons = {}
    for case in vectors["cases"]:
        outcome = check_case(case, base, problems)
        print("%-22s %-7s %-12s %s" % (case["name"], case["kind"], case.get("reason", "") or "-", outcome))
        if outcome != "ok":
            problems.append(case["name"] + ": " + outcome)
        if case.get("reason"):
            reasons[case["reason"]] = reasons.get(case["reason"], 0) + 1
    total = sum(os.path.getsize(os.path.join(dp, fn)) for dp, _, fns in os.walk(base) if "fuzz" not in dp for fn in fns)
    if total >= 1_000_000:
        problems.append("the vector files hold %d bytes" % total)
    print()
    print("cases: %d, negative by reason: %s, files: %d bytes" % (len(vectors["cases"]), dict(sorted(reasons.items())), total))
    for p in problems:
        print("PROBLEM:", p)
    print("every case agrees" if not problems else "%d problems" % len(problems))
    return 0 if not problems else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
