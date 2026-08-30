from sqlalchemy import select

from app.models.listing import Listing
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


async def test_list_users_requires_admin(client):
    token, _ = await _signup(client, "plain@gmail.com")
    res = await client.get("/api/v1/users", headers=_auth_headers(token))
    assert res.status_code == 403


async def test_list_users_returns_everyone(client, db_session):
    admin_token, admin_user = await _signup(client, "admin@gmail.com")
    await _make_admin(db_session, admin_user["id"])
    await _signup(client, "someone@gmail.com")

    res = await client.get("/api/v1/users", headers=_auth_headers(admin_token))
    assert res.status_code == 200, res.text
    emails = {u["email"] for u in res.json()}
    assert {"admin@gmail.com", "someone@gmail.com"} <= emails


async def test_set_admin_promotes_and_demotes(client, db_session):
    admin_token, admin_user = await _signup(client, "boss@gmail.com")
    await _make_admin(db_session, admin_user["id"])
    _, target = await _signup(client, "newmod@gmail.com")

    promote = await client.patch(
        f"/api/v1/users/{target['id']}/admin",
        headers=_auth_headers(admin_token),
        json={"is_admin": True},
    )
    assert promote.status_code == 200, promote.text
    assert promote.json()["is_admin"] is True

    demote = await client.patch(
        f"/api/v1/users/{target['id']}/admin",
        headers=_auth_headers(admin_token),
        json={"is_admin": False},
    )
    assert demote.status_code == 200
    assert demote.json()["is_admin"] is False


async def test_admin_cannot_remove_own_access(client, db_session):
    admin_token, admin_user = await _signup(client, "onlyadmin@gmail.com")
    await _make_admin(db_session, admin_user["id"])

    res = await client.patch(
        f"/api/v1/users/{admin_user['id']}/admin",
        headers=_auth_headers(admin_token),
        json={"is_admin": False},
    )
    assert res.status_code == 400


async def test_set_admin_requires_admin(client):
    token, _ = await _signup(client, "notadmin@gmail.com")
    _, other = await _signup(client, "victim@gmail.com")
    res = await client.patch(
        f"/api/v1/users/{other['id']}/admin",
        headers=_auth_headers(token),
        json={"is_admin": True},
    )
    assert res.status_code == 403


async def test_report_response_includes_reporter_email_and_target_label(client, db_session):
    reporter_token, _ = await _signup(client, "reporter2@gmail.com")
    admin_token, admin_user = await _signup(client, "modadmin@gmail.com")
    await _make_admin(db_session, admin_user["id"])

    listing = Listing(
        title="Cozy studio",
        type="Studio",
        price_per=800,
        total=800,
        deposit=800,
        walk=5,
        grad="from-coral to-sunshine",
        emoji="🏠",
    )
    db_session.add(listing)
    await db_session.commit()
    await db_session.refresh(listing)

    create = await client.post(
        "/api/v1/reports",
        headers=_auth_headers(reporter_token),
        json={"target_type": "listing", "target_id": listing.id, "reason": "spam"},
    )
    assert create.status_code == 201, create.text
    assert create.json()["reporter_email"] == "reporter2@gmail.com"
    assert create.json()["target_label"] == "Cozy studio"

    listed = await client.get("/api/v1/reports", headers=_auth_headers(admin_token))
    assert listed.status_code == 200
    row = next(r for r in listed.json() if r["target_id"] == listing.id)
    assert row["reporter_email"] == "reporter2@gmail.com"
    assert row["target_label"] == "Cozy studio"
