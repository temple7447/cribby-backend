from datetime import datetime, timedelta, timezone

from sqlalchemy import select

import app.api.v1.endpoints.auth as auth_endpoint
from app.models.user import User


async def _signup(client, email="student@stateuniversity.edu", password="pw123456", name="Test"):
    res = await client.post(
        "/api/v1/auth/signup",
        json={"email": email, "password": password, "name": name},
    )
    assert res.status_code == 200, res.text
    body = res.json()
    return body["access_token"], body["user"]


def _auth_headers(token: str) -> dict:
    return {"Authorization": f"Bearer {token}"}


async def test_signup_creates_unverified_user(client):
    _, user = await _signup(client)
    assert user["verification_status"] == "unverified"


async def test_request_verification_accepts_any_email_domain(client, monkeypatch):
    # .edu isn't a realistic constraint outside the US (e.g. Nigerian universities
    # don't issue them), so verification accepts any address — gmail included.
    captured = {}
    monkeypatch.setattr(
        auth_endpoint, "send_verification_email", lambda to_email, code: captured.update(email=to_email, code=code)
    )
    token, _ = await _signup(client, email="student@gmail.com")
    res = await client.post("/api/v1/auth/verify/request", headers=_auth_headers(token))
    assert res.status_code == 204, res.text
    assert captured["email"] == "student@gmail.com"


async def test_request_verification_and_confirm_succeeds(client, monkeypatch):
    captured = {}

    def fake_send(to_email, code):
        captured["email"] = to_email
        captured["code"] = code

    monkeypatch.setattr(auth_endpoint, "send_verification_email", fake_send)

    token, _ = await _signup(client)
    res = await client.post("/api/v1/auth/verify/request", headers=_auth_headers(token))
    assert res.status_code == 204
    assert captured["code"] is not None

    res = await client.post(
        "/api/v1/auth/verify/confirm", json={"code": captured["code"]}, headers=_auth_headers(token)
    )
    assert res.status_code == 200, res.text
    assert res.json()["verification_status"] == "verified"


async def test_confirm_wrong_code_fails(client, monkeypatch):
    monkeypatch.setattr(auth_endpoint, "send_verification_email", lambda *_: None)
    token, _ = await _signup(client)
    await client.post("/api/v1/auth/verify/request", headers=_auth_headers(token))
    res = await client.post(
        "/api/v1/auth/verify/confirm", json={"code": "000000"}, headers=_auth_headers(token)
    )
    assert res.status_code == 400


async def test_confirm_expired_code_fails(client, db_session, monkeypatch):
    captured = {}
    monkeypatch.setattr(
        auth_endpoint, "send_verification_email", lambda email, code: captured.update(code=code)
    )
    token, user = await _signup(client)
    await client.post("/api/v1/auth/verify/request", headers=_auth_headers(token))

    db_user = (
        await db_session.execute(select(User).where(User.id == user["id"]))
    ).scalar_one()
    db_user.verification_code_expires_at = datetime.now(timezone.utc) - timedelta(minutes=1)
    await db_session.commit()

    res = await client.post(
        "/api/v1/auth/verify/confirm", json={"code": captured["code"]}, headers=_auth_headers(token)
    )
    assert res.status_code == 400


async def test_resend_cooldown_returns_429(client, monkeypatch):
    monkeypatch.setattr(auth_endpoint, "send_verification_email", lambda *_: None)
    token, _ = await _signup(client)
    res1 = await client.post("/api/v1/auth/verify/request", headers=_auth_headers(token))
    assert res1.status_code == 204
    res2 = await client.post("/api/v1/auth/verify/request", headers=_auth_headers(token))
    assert res2.status_code == 429


async def test_self_verify_via_patch_users_me_is_rejected(client):
    token, _ = await _signup(client)
    res = await client.patch(
        "/api/v1/users/me",
        json={"verification_status": "verified"},
        headers=_auth_headers(token),
    )
    assert res.status_code == 200

    me = await client.get("/api/v1/users/me", headers=_auth_headers(token))
    assert me.json()["verification_status"] == "unverified"
