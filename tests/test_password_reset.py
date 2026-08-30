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


async def test_request_reset_for_unknown_email_still_returns_204(client, monkeypatch):
    called = []
    monkeypatch.setattr(auth_endpoint, "send_password_reset_email", lambda *a: called.append(a))

    res = await client.post(
        "/api/v1/auth/password-reset/request", json={"email": "nobody@stateuniversity.edu"}
    )
    assert res.status_code == 204
    assert called == []  # never actually sends for a nonexistent account


async def test_request_and_confirm_reset_succeeds(client, monkeypatch):
    await _signup(client, email="student@stateuniversity.edu", password="oldpass123")

    captured = {}
    monkeypatch.setattr(
        auth_endpoint,
        "send_password_reset_email",
        lambda email, code: captured.update(email=email, code=code),
    )

    res = await client.post(
        "/api/v1/auth/password-reset/request", json={"email": "student@stateuniversity.edu"}
    )
    assert res.status_code == 204
    assert captured["email"] == "student@stateuniversity.edu"
    code = captured["code"]

    res2 = await client.post(
        "/api/v1/auth/password-reset/confirm",
        json={"email": "student@stateuniversity.edu", "code": code, "new_password": "newpass456"},
    )
    assert res2.status_code == 200, res2.text
    body = res2.json()
    assert body["user"]["email"] == "student@stateuniversity.edu"
    assert "access_token" in body

    # old password no longer works, new one does
    old_login = await client.post(
        "/api/v1/auth/login",
        json={"email": "student@stateuniversity.edu", "password": "oldpass123"},
    )
    assert old_login.status_code == 401

    new_login = await client.post(
        "/api/v1/auth/login",
        json={"email": "student@stateuniversity.edu", "password": "newpass456"},
    )
    assert new_login.status_code == 200


async def test_confirm_wrong_code_fails(client, monkeypatch):
    await _signup(client, email="student@stateuniversity.edu")
    monkeypatch.setattr(auth_endpoint, "send_password_reset_email", lambda *_: None)
    await client.post(
        "/api/v1/auth/password-reset/request", json={"email": "student@stateuniversity.edu"}
    )

    res = await client.post(
        "/api/v1/auth/password-reset/confirm",
        json={"email": "student@stateuniversity.edu", "code": "000000", "new_password": "whatever1"},
    )
    assert res.status_code == 400


async def test_confirm_without_a_pending_request_fails(client):
    await _signup(client, email="student@stateuniversity.edu")

    res = await client.post(
        "/api/v1/auth/password-reset/confirm",
        json={"email": "student@stateuniversity.edu", "code": "123456", "new_password": "whatever1"},
    )
    assert res.status_code == 400


async def test_confirm_expired_code_fails(client, db_session, monkeypatch):
    await _signup(client, email="student@stateuniversity.edu")
    captured = {}
    monkeypatch.setattr(
        auth_endpoint, "send_password_reset_email", lambda email, code: captured.update(code=code)
    )
    await client.post(
        "/api/v1/auth/password-reset/request", json={"email": "student@stateuniversity.edu"}
    )

    user = (
        await db_session.execute(select(User).where(User.email == "student@stateuniversity.edu"))
    ).scalar_one()
    user.password_reset_code_expires_at = datetime.now(timezone.utc) - timedelta(minutes=1)
    await db_session.commit()

    res = await client.post(
        "/api/v1/auth/password-reset/confirm",
        json={
            "email": "student@stateuniversity.edu",
            "code": captured["code"],
            "new_password": "whatever1",
        },
    )
    assert res.status_code == 400


async def test_resend_cooldown_returns_no_new_email(client, monkeypatch):
    await _signup(client, email="student@stateuniversity.edu")
    calls = []
    monkeypatch.setattr(
        auth_endpoint, "send_password_reset_email", lambda *a: calls.append(a)
    )

    res1 = await client.post(
        "/api/v1/auth/password-reset/request", json={"email": "student@stateuniversity.edu"}
    )
    assert res1.status_code == 204
    assert len(calls) == 1

    # immediate second request — still within cooldown, should not send again
    res2 = await client.post(
        "/api/v1/auth/password-reset/request", json={"email": "student@stateuniversity.edu"}
    )
    assert res2.status_code == 204
    assert len(calls) == 1


async def test_confirm_rejects_short_password(client, monkeypatch):
    await _signup(client, email="student@stateuniversity.edu")
    captured = {}
    monkeypatch.setattr(
        auth_endpoint, "send_password_reset_email", lambda email, code: captured.update(code=code)
    )
    await client.post(
        "/api/v1/auth/password-reset/request", json={"email": "student@stateuniversity.edu"}
    )

    res = await client.post(
        "/api/v1/auth/password-reset/confirm",
        json={"email": "student@stateuniversity.edu", "code": captured["code"], "new_password": "abc"},
    )
    assert res.status_code == 422
