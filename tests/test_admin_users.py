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


async def _create_listing(db_session) -> int:
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
    return listing.id


async def test_user_detail_requires_admin(client):
    token, other = await _signup(client, "plain2@gmail.com")
    res = await client.get(f"/api/v1/users/{other['id']}", headers=_auth_headers(token))
    assert res.status_code == 403


async def test_user_detail_404_for_unknown_user(client, db_session):
    admin_token, admin_user = await _signup(client, "admin2@gmail.com")
    await _make_admin(db_session, admin_user["id"])
    res = await client.get("/api/v1/users/999999", headers=_auth_headers(admin_token))
    assert res.status_code == 404


async def test_user_detail_includes_bookings_profile_and_reports(client, db_session):
    admin_token, admin_user = await _signup(client, "admin3@gmail.com")
    await _make_admin(db_session, admin_user["id"])
    student_token, student = await _signup(client, "student3@gmail.com")
    student_row = (
        await db_session.execute(select(User).where(User.id == student["id"]))
    ).scalar_one()
    student_row.verification_status = "verified"
    await db_session.commit()

    listing_id = await _create_listing(db_session)
    booking_res = await client.post(
        "/api/v1/bookings",
        headers=_auth_headers(student_token),
        json={"listing_id": listing_id, "move_in_date": "2026-09-01", "lease_months": 12},
    )
    assert booking_res.status_code == 201, booking_res.text

    profile_res = await client.post(
        "/api/v1/roommates",
        headers=_auth_headers(student_token),
        json={
            "name": "Zoe",
            "age": 20,
            "school": "State University",
            "year": "Sophomore",
            "budget_min": 500,
            "budget_max": 900,
            "move_in_date": "Aug 1",
        },
    )
    assert profile_res.status_code == 201, profile_res.text

    report_res = await client.post(
        "/api/v1/reports",
        headers=_auth_headers(admin_token),
        json={"target_type": "user", "target_id": student["id"], "reason": "spam"},
    )
    assert report_res.status_code == 201, report_res.text

    detail = await client.get(f"/api/v1/users/{student['id']}", headers=_auth_headers(admin_token))
    assert detail.status_code == 200, detail.text
    body = detail.json()
    assert body["user"]["email"] == "student3@gmail.com"
    assert len(body["bookings"]) == 1
    assert body["bookings"][0]["listing"]["id"] == listing_id
    assert body["roommate_profile"]["name"] == "Zoe"
    assert body["reports_filed"] == []
    assert len(body["reports_against"]) == 1
    assert body["reports_against"][0]["reporter_email"] == "admin3@gmail.com"


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
