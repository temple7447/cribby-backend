from sqlalchemy import select

from app.models.user import User


async def _signup(client, email, password="pw123456", name="Test"):
    res = await client.post(
        "/api/v1/auth/signup", json={"email": email, "password": password, "name": name}
    )
    assert res.status_code == 200, res.text
    body = res.json()
    return body["access_token"], body["user"]


def _auth_headers(token: str) -> dict:
    return {"Authorization": f"Bearer {token}"}


async def _make_admin(db_session, user_id: int) -> None:
    user = (await db_session.execute(select(User).where(User.id == user_id))).scalar_one()
    user.is_admin = True
    await db_session.commit()


async def test_set_suspended_requires_admin(client):
    token, _ = await _signup(client, "notadmin4@gmail.com")
    _, other = await _signup(client, "victim2@gmail.com")
    res = await client.patch(
        f"/api/v1/users/{other['id']}/suspend",
        headers=_auth_headers(token),
        json={"is_suspended": True},
    )
    assert res.status_code == 403


async def test_admin_cannot_suspend_own_account(client, db_session):
    admin_token, admin_user = await _signup(client, "selfsuspend@gmail.com")
    await _make_admin(db_session, admin_user["id"])

    res = await client.patch(
        f"/api/v1/users/{admin_user['id']}/suspend",
        headers=_auth_headers(admin_token),
        json={"is_suspended": True},
    )
    assert res.status_code == 400


async def test_suspend_blocks_login(client, db_session):
    admin_token, admin_user = await _signup(client, "modadmin2@gmail.com")
    await _make_admin(db_session, admin_user["id"])
    _, target = await _signup(client, "suspendme@gmail.com", password="pw123456")

    suspend = await client.patch(
        f"/api/v1/users/{target['id']}/suspend",
        headers=_auth_headers(admin_token),
        json={"is_suspended": True},
    )
    assert suspend.status_code == 200, suspend.text
    assert suspend.json()["is_suspended"] is True

    login = await client.post(
        "/api/v1/auth/login",
        json={"email": "suspendme@gmail.com", "password": "pw123456"},
    )
    assert login.status_code == 403
    assert "suspended" in login.json()["detail"].lower()


async def test_suspend_cuts_off_an_existing_session(client, db_session):
    admin_token, admin_user = await _signup(client, "modadmin3@gmail.com")
    await _make_admin(db_session, admin_user["id"])
    target_token, target = await _signup(client, "midsession@gmail.com")

    # Token was issued before suspension — still works until the flag flips.
    ok = await client.get("/api/v1/users/me", headers=_auth_headers(target_token))
    assert ok.status_code == 200

    await client.patch(
        f"/api/v1/users/{target['id']}/suspend",
        headers=_auth_headers(admin_token),
        json={"is_suspended": True},
    )

    blocked = await client.get("/api/v1/users/me", headers=_auth_headers(target_token))
    assert blocked.status_code == 403
    assert "suspended" in blocked.json()["detail"].lower()


async def test_unsuspend_restores_access(client, db_session):
    admin_token, admin_user = await _signup(client, "modadmin4@gmail.com")
    await _make_admin(db_session, admin_user["id"])
    _, target = await _signup(client, "restoreme@gmail.com", password="pw123456")

    await client.patch(
        f"/api/v1/users/{target['id']}/suspend",
        headers=_auth_headers(admin_token),
        json={"is_suspended": True},
    )
    unsuspend = await client.patch(
        f"/api/v1/users/{target['id']}/suspend",
        headers=_auth_headers(admin_token),
        json={"is_suspended": False},
    )
    assert unsuspend.status_code == 200
    assert unsuspend.json()["is_suspended"] is False

    login = await client.post(
        "/api/v1/auth/login",
        json={"email": "restoreme@gmail.com", "password": "pw123456"},
    )
    assert login.status_code == 200, login.text
