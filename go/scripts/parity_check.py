#!/usr/bin/env python3
"""Differential test: fire the same requests at the original FastAPI app and the Go
port, then compare status codes and JSON bodies.

Setup (both servers must start from IDENTICAL copies of a seeded database, with the
demo user promoted to admin so the admin endpoints can be exercised):

    cp campus.db py.db && cp campus.db go.db
    sqlite3 py.db "update users set is_admin=1 where id=1;"   # same for go.db
    # FastAPI:  DATABASE_URL=sqlite+aiosqlite:///./py.db SECRET_KEY=x uvicorn app.main:app --port 8801
    # Go:       DATABASE_URL=sqlite+aiosqlite:///./go.db SECRET_KEY=x PORT=8802 ./server
    python3 scripts/parity_check.py http://localhost:8801/api/v1 http://localhost:8802/api/v1

It MUTATES both databases (signups, bookings, reports, deletions…), so always point it
at throw-away copies, never at production. The sequence assumes the `seed demo` dataset.
Rows created in the same second can legitimately come back in a different order
(`ORDER BY created_at DESC` has no tie-break in the original); those are reported as
"order-only", not as differences.
"""
import json, sys, urllib.request, urllib.error

PY = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8801/api/v1"
GO = sys.argv[2] if len(sys.argv) > 2 else "http://localhost:8802/api/v1"
VOLATILE = {"created_at", "access_token", "resolved_at", "uploaded_at"}

def call(base, method, path, token=None, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, method=method)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req) as r:
            raw, status = r.read(), r.status
    except urllib.error.HTTPError as e:
        raw, status = e.read(), e.code
    try:
        return status, json.loads(raw) if raw else None
    except Exception:
        return status, raw.decode()

def norm(v, key=None):
    if isinstance(v, dict):
        return {k: ("<volatile>" if k in VOLATILE and x is not None else norm(x, k)) for k, x in v.items()}
    if isinstance(v, list):
        return [norm(x) for x in v]
    return v

def by_id(v):
    if isinstance(v, list) and v and all(isinstance(x, dict) and "id" in x for x in v):
        return sorted((by_id(x) for x in v), key=lambda x: x["id"])
    if isinstance(v, dict):
        return {k: by_id(x) for k, x in v.items()}
    return v

def shape422(body):
    # compare only what clients can rely on: 422 + the list of (loc) entries
    if isinstance(body, dict) and isinstance(body.get("detail"), list):
        return sorted(tuple(map(str, d.get("loc", []))) for d in body["detail"])
    return body

results = {"same": 0, "diff": 0}
tok = {}

def both(label, method, path, who=None, body=None):
    sp, bp = call(PY, method, path, tok.get((who, "py")), body)
    sg, bg = call(GO, method, path, tok.get((who, "go")), body)
    if sp == 422 and sg == 422:
        np, ng = shape422(bp), shape422(bg)
    else:
        np, ng = norm(bp), norm(bg)
    if sp == sg and np == ng:
        results["same"] += 1
        print(f"  same  {sp} {method} {path}")
    elif sp == sg and by_id(np) == by_id(ng):
        results["order"] = results.get("order", 0) + 1
        print(f"~ ORDER-ONLY (same rows, tie order differs)  {sp} {method} {path}")
    else:
        results["diff"] += 1
        print(f"! DIFF  {label}: {method} {path}\n    python {sp}: {json.dumps(np, ensure_ascii=False)[:600]}\n    go     {sg}: {json.dumps(ng, ensure_ascii=False)[:600]}")
    return bp, bg

def login(who, email, pw):
    for side, base in (("py", PY), ("go", GO)):
        s, b = call(base, "POST", "/auth/login", None, {"email": email, "password": pw})
        assert s == 200, (side, s, b)
        tok[(who, side)] = b["access_token"]

login("maya", "maya@stateuniversity.edu", "password123")
login("zoe", "zoe@stateuniversity.edu", "password123")

print("== public reads")
for p in ["/listings", "/listings?max_price=900&room_type=studio", "/listings?q=loft", "/listings?max_walk=10",
          "/listings?near_lat=6.5158&near_lng=3.3898", "/listings?near_lat=6.5158&near_lng=3.3898&radius_km=0.8",
          "/listings/1", "/listings/3", "/listings/999", "/roommates", "/roommates/1", "/roommates/999",
          "/places", "/places?category=pharmacy&near_lat=6.5158&near_lng=3.3898", "/places?q=bank", "/places/1", "/places/999"]:
    both("public", "GET", p)

print("== auth & errors")
for p in ["/users/me", "/bookings"]:
    both("noauth", "GET", p)
tok[("bad", "py")] = tok[("bad", "go")] = "garbage"
both("badtoken", "GET", "/users/me", "bad")
both("422", "GET", "/listings?max_price=abc")
both("422", "GET", "/listings/abc")
both("422", "POST", "/auth/signup", None, {"email": "nope", "password": "x"})
both("422", "POST", "/auth/login", None, {})
both("401", "POST", "/auth/login", None, {"email": "maya@stateuniversity.edu", "password": "wrong"})
both("404", "GET", "/does-not-exist")

print("== authenticated reads (maya = admin)")
for p in ["/users/me", "/users", "/users/2", "/users/999", "/bookings", "/bookings?status_filter=approved", "/bookings/1",
          "/bookings/999", "/bookings/pending", "/conversations", "/conversations/1", "/conversations/999", "/favorites",
          "/blocks", "/reports", "/reports?status=pending", "/reports?status=bogus", "/roommates/me"]:
    both("authed", "GET", p, "maya")
both("zoe", "GET", "/roommates/me", "zoe")
both("zoe", "GET", "/users", "zoe")

print("== mutations (same sequence on both)")
both("signup", "POST", "/auth/signup", None, {"email": "New.Person@Gmail.com", "password": "pw123456", "name": "New Person"})
both("signup-dup", "POST", "/auth/signup", None, {"email": "New.Person@Gmail.com", "password": "pw123456", "name": "New Person"})
login("new", "New.Person@gmail.com", "pw123456")
both("me", "GET", "/users/me", "new")
both("patch-me", "PATCH", "/users/me", "new", {"name": "Renamed", "avatar_emoji": "🦊", "verification_status": "verified"})
both("verify-req", "POST", "/auth/verify/request", "new")
both("verify-req-429", "POST", "/auth/verify/request", "new")
both("verify-bad", "POST", "/auth/verify/confirm", "new", {"code": "000000"})
both("reset-unknown", "POST", "/auth/password-reset/request", None, {"email": "ghost@gmail.com"})
both("reset-422", "POST", "/auth/password-reset/confirm", None, {"email": "a@b.co", "code": "1", "new_password": "x"})
both("reset-400", "POST", "/auth/password-reset/confirm", None, {"email": "a@b.co", "code": "123456", "new_password": "longenough"})
both("booking", "POST", "/bookings", "maya", {"listing_id": 3, "move_in_date": "2026-12-01", "lease_months": 6, "guarantor_email": "Mum@Example.com", "guarantor_name": "Mum", "message_to_landlord": "hi"})
both("booking-404", "POST", "/bookings", "maya", {"listing_id": 999, "move_in_date": "2026-12-01"})
both("booking-422", "POST", "/bookings", "maya", {"listing_id": 3, "move_in_date": "soon"})
both("pending", "GET", "/bookings/pending", "maya")
both("decide", "PATCH", "/bookings/3", "maya", {"status": "approved"})
both("decide-again", "PATCH", "/bookings/3", "maya", {"status": "rejected"})
both("decide-422", "PATCH", "/bookings/1", "maya", {"status": "maybe"})
both("cancel", "PATCH", "/bookings/3/cancel", "maya")
both("cancel-again", "PATCH", "/bookings/3/cancel", "maya")
both("fav-put", "PUT", "/favorites/2", "maya")
both("fav-put-again", "PUT", "/favorites/2", "maya")
both("fav-get", "GET", "/favorites", "maya")
both("fav-del", "DELETE", "/favorites/2", "maya")
both("block", "POST", "/blocks", "maya", {"blocked_user_id": 3})
both("block-again", "POST", "/blocks", "maya", {"blocked_user_id": 3})
both("block-self", "POST", "/blocks", "maya", {"blocked_user_id": 1})
both("block-404", "POST", "/blocks", "maya", {"blocked_user_id": 999})
both("blocks", "GET", "/blocks", "maya")
both("unblock", "DELETE", "/blocks/3", "maya")
both("convo-landlord", "POST", "/conversations", "maya", {"kind": "landlord", "listing_id": 2})
both("convo-landlord-again", "POST", "/conversations", "maya", {"kind": "landlord", "listing_id": 2})
both("convo-roommate", "POST", "/conversations", "maya", {"kind": "roommate", "roommate_profile_id": 2})
both("convo-roommate-self", "POST", "/conversations", "zoe", {"kind": "roommate", "roommate_profile_id": 1})
both("send", "POST", "/conversations/3/messages", "maya", {"text": "Is it available?"})
both("convo-get", "GET", "/conversations/3", "maya")
both("convos", "GET", "/conversations", "maya")
both("report-listing", "POST", "/reports", "zoe", {"target_type": "listing", "target_id": 2, "reason": "spam", "details": "dup"})
both("report-bad", "POST", "/reports", "zoe", {"target_type": "nope", "target_id": 2, "reason": "spam"})
both("report-404", "POST", "/reports", "zoe", {"target_type": "listing", "target_id": 999, "reason": "spam"})
both("report-user", "POST", "/reports", "zoe", {"target_type": "user", "target_id": 3, "reason": "scam"})
both("listings-after-report", "GET", "/listings")
both("reports", "GET", "/reports", "maya")
both("report-patch", "PATCH", "/reports/1", "maya", {"status": "dismissed"})
both("listing-after-dismiss", "GET", "/listings/2")
both("report-resolve", "PATCH", "/reports/2", "maya", {"status": "resolved"})
both("listing-create", "POST", "/listings", "maya", {"title": "Diff Test Loft", "type": "Loft", "price_per": 700, "total": 700, "deposit": 700, "walk": 9, "latitude": 6.52, "longitude": 3.39, "amenities": ["📶 Wi-Fi"]})
both("listing-create-422", "POST", "/listings", "maya", {"title": "no price"})
both("listing-create-403", "POST", "/listings", "zoe", {"title": "x", "type": "x", "price_per": 1, "total": 1, "deposit": 1, "walk": 1})
both("place-create", "POST", "/places", "maya", {"name": "Diff Pharmacy", "category": "PHARMACY", "latitude": 6.516, "longitude": 3.39, "address": "1 Test St"})
both("place-search", "GET", "/places?category=pharmacy&near_lat=6.5158&near_lng=3.3898")
both("place-del", "DELETE", "/places/19", "maya")
both("place-del-again", "DELETE", "/places/19", "maya")
both("rm-create", "POST", "/roommates", "maya", {"name": "Maya", "age": 21, "school": "State University", "year": "Junior", "budget_min": 400, "budget_max": 700, "move_in_date": "Sep 1", "tags": ["a"], "bio": "hello"})
both("rm-create-dup", "POST", "/roommates", "maya", {"name": "Maya", "age": 21, "school": "S", "year": "Junior", "budget_min": 400, "budget_max": 700, "move_in_date": "Sep 1"})
both("rm-unverified", "POST", "/roommates", "new", {"name": "N", "age": 21, "school": "S", "year": "Junior", "budget_min": 4, "budget_max": 7, "move_in_date": "Sep 1"})
both("rm-me", "GET", "/roommates/me", "maya")
both("rm-patch", "PATCH", "/roommates/me", "maya", {"bio": "updated", "visibility": "paused", "preferred_areas": ["North Gate"]})
both("rm-patch-bad", "PATCH", "/roommates/me", "maya", {"visibility": "removed"})
both("rm-list", "GET", "/roommates")
both("rm-get", "GET", "/roommates/5")
both("admin-promote", "PATCH", "/users/2/admin", "maya", {"is_admin": True})
both("admin-self", "PATCH", "/users/1/admin", "maya", {"is_admin": False})
both("suspend", "PATCH", "/users/4/suspend", "maya", {"is_suspended": True})
both("suspend-self", "PATCH", "/users/1/suspend", "maya", {"is_suspended": True})
both("users", "GET", "/users", "maya")
both("user-detail", "GET", "/users/1", "maya")
both("avatar-noauth", "POST", "/users/me/avatar")
both("avatar-delete", "DELETE", "/users/me/avatar", "maya")
both("delete-wrong-pw", "DELETE", "/users/me", "new", {"password": "wrong"})
both("delete-ok", "DELETE", "/users/me", "new", {"password": "pw123456"})
both("deleted-user-token", "GET", "/users/me", "new")


print("== raw requests: CORS, multipart, odd methods")
def raw(base, method, path, headers=None, data=None):
    req = urllib.request.Request(base + path, data=data, method=method)
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req) as r:
            return r.status, {k.lower(): v for k, v in r.headers.items()}, r.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, {k.lower(): v for k, v in e.headers.items()}, e.read().decode()

CORS_KEYS = ["access-control-allow-origin", "access-control-allow-credentials", "access-control-allow-methods",
             "access-control-allow-headers", "access-control-max-age", "vary"]
def raw_both(label, method, path, headers=None, data=None, keys=CORS_KEYS, body=False):
    a, b = raw(PY, method, path, headers, data), raw(GO, method, path, headers, data)
    # Newer Starlette adds the HTTP QUERY method to its CORS list; the API doesn't use it.
    ha = {k: (a[1].get(k) or "").replace(", QUERY", "") or None for k in keys}
    hb = {k: (b[1].get(k) or "").replace(", QUERY", "") or None for k in keys}
    same = a[0] == b[0] and ha == hb and (not body or a[2] == b[2])
    results["same" if same else "diff"] += 1
    if same:
        print(f"  same  {a[0]} {method} {path} [{label}]")
    else:
        print(f"! DIFF  {label}: {method} {path}\n    python {a[0]} {ha} {a[2][:120]!r}\n    go     {b[0]} {hb} {b[2][:120]!r}")


for origin in ["http://localhost:5180", "https://admin.cribby.space", "http://192.168.1.20:8081", "http://10.1.2.3:19006",
               "http://172.20.0.5:3000", "https://evil.example.com", "http://172.40.0.5:3000", "https://192.168.1.20:8081"]:
    raw_both("cors-simple", "GET", "/listings/1", {"Origin": origin})
    raw_both("cors-preflight", "OPTIONS", "/listings", {"Origin": origin, "Access-Control-Request-Method": "POST",
             "Access-Control-Request-Headers": "authorization,content-type"}, body=True)
raw_both("no-origin", "GET", "/listings/1")
raw_both("wrong-method", "PUT", "/listings/1", keys=["content-type"], body=True)

import uuid
def multipart(filename, ctype, payload, field="file"):
    boundary = uuid.uuid4().hex
    body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"{field}\"; filename=\"{filename}\"\r\nContent-Type: {ctype}\r\n\r\n").encode() + payload + f"\r\n--{boundary}--\r\n".encode()
    return {"Content-Type": f"multipart/form-data; boundary={boundary}"}, body
for label, who, args, path in [
    ("avatar-unconfigured", "maya", ("a.jpg", "image/jpeg", b"x"), "/users/me/avatar"),
    ("avatar-badtype-unconfigured", "maya", ("a.exe", "application/octet-stream", b"x"), "/users/me/avatar"),
    ("avatar-nofile", "maya", ("a.jpg", "image/jpeg", b"x", "wrongfield"), "/users/me/avatar"),
    ("avatar-noauth", None, ("a.jpg", "image/jpeg", b"x"), "/users/me/avatar"),
    ("listing-photo-unconfigured", "maya", ("a.jpg", "image/jpeg", b"x"), "/listings/1/photos"),
    ("listing-photo-404", "maya", ("a.jpg", "image/jpeg", b"x"), "/listings/999/photos"),
    ("listing-photo-nonadmin", "zoe", ("a.jpg", "image/jpeg", b"x"), "/listings/1/photos"),
    ("banner-noprofile", "zoe_none", ("a.jpg", "image/jpeg", b"x"), "/roommates/me/banner"),
]:
    results_before = dict(results)
    outs = []
    for side, base in (("py", PY), ("go", GO)):
        h, d = multipart(*args)
        if who and (who, side) in tok:
            h["Authorization"] = "Bearer " + tok[(who, side)]
        outs.append(raw(base, "POST", path, h, d))
    (sa, _, ba), (sg, _, bg) = outs
    ja = json.loads(ba) if ba else None
    jg = json.loads(bg) if bg else None
    if sa == 422 and sg == 422:
        ja, jg = shape422(ja), shape422(jg)
    if sa == sg and ja == jg:
        results["same"] += 1; print(f"  same  {sa} POST {path} [{label}]")
    else:
        results["diff"] += 1; print(f"! DIFF  {label}: POST {path}\n    python {sa}: {ba[:200]}\n    go     {sg}: {bg[:200]}")

print(f"\nSUMMARY: {results['same']} identical, {results.get('order',0)} order-only, {results['diff']} different")
sys.exit(1 if results["diff"] else 0)
