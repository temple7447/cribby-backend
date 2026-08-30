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


async def _create_booking(client, token, listing_id) -> dict:
    res = await client.post(
        "/api/v1/bookings",
        headers=_auth_headers(token),
        json={"listing_id": listing_id, "move_in_date": "2026-09-01", "lease_months": 12},
    )
    assert res.status_code == 201, res.text
    return res.json()


async def test_booking_starts_pending_not_auto_approved(client, db_session):
    token, _ = await _signup(client, "student@stateuniversity.edu")
    listing_id = await _create_listing(db_session)

    booking = await _create_booking(client, token, listing_id)
    assert booking["status"] == "pending"
    assert booking["landlord_approved"] is False


async def test_pending_list_requires_admin(client, db_session):
    token, _ = await _signup(client, "student@stateuniversity.edu")
    listing_id = await _create_listing(db_session)
    await _create_booking(client, token, listing_id)

    res = await client.get("/api/v1/bookings/pending", headers=_auth_headers(token))
    assert res.status_code == 403


async def test_admin_sees_pending_booking_with_student_info(client, db_session):
    student_token, student = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    res = await client.get("/api/v1/bookings/pending", headers=_auth_headers(admin_token))
    assert res.status_code == 200, res.text
    pending = res.json()
    assert len(pending) == 1
    assert pending[0]["id"] == booking["id"]
    assert pending[0]["user"]["email"] == "student@stateuniversity.edu"


async def test_admin_can_approve_booking(client, db_session):
    student_token, student = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}",
        headers=_auth_headers(admin_token),
        json={"status": "approved"},
    )
    assert res.status_code == 200, res.text
    body = res.json()
    assert body["status"] == "approved"
    assert body["landlord_approved"] is True

    # student sees the updated status
    res2 = await client.get(f"/api/v1/bookings/{booking['id']}", headers=_auth_headers(student_token))
    assert res2.json()["status"] == "approved"

    # no longer in the pending queue
    res3 = await client.get("/api/v1/bookings/pending", headers=_auth_headers(admin_token))
    assert res3.json() == []


async def test_admin_can_reject_booking(client, db_session):
    student_token, student = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}",
        headers=_auth_headers(admin_token),
        json={"status": "rejected"},
    )
    assert res.status_code == 200, res.text
    body = res.json()
    assert body["status"] == "rejected"
    assert body["landlord_approved"] is False


async def test_decide_requires_admin(client, db_session):
    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}",
        headers=_auth_headers(student_token),
        json={"status": "approved"},
    )
    assert res.status_code == 403


async def test_cannot_decide_an_already_decided_booking(client, db_session):
    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    await client.patch(
        f"/api/v1/bookings/{booking['id']}",
        headers=_auth_headers(admin_token),
        json={"status": "approved"},
    )
    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}",
        headers=_auth_headers(admin_token),
        json={"status": "rejected"},
    )
    assert res.status_code == 400


async def test_decide_rejects_invalid_status_value(client, db_session):
    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}",
        headers=_auth_headers(admin_token),
        json={"status": "not-a-real-status"},
    )
    assert res.status_code == 422


async def test_student_can_cancel_pending_booking(client, db_session):
    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}/cancel", headers=_auth_headers(student_token)
    )
    assert res.status_code == 200, res.text
    assert res.json()["status"] == "canceled"


async def test_student_can_cancel_approved_booking(client, db_session):
    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)
    await client.patch(
        f"/api/v1/bookings/{booking['id']}", headers=_auth_headers(admin_token), json={"status": "approved"}
    )

    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}/cancel", headers=_auth_headers(student_token)
    )
    assert res.status_code == 200, res.text
    assert res.json()["status"] == "canceled"


async def test_cannot_cancel_a_rejected_booking(client, db_session):
    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)
    await client.patch(
        f"/api/v1/bookings/{booking['id']}", headers=_auth_headers(admin_token), json={"status": "rejected"}
    )

    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}/cancel", headers=_auth_headers(student_token)
    )
    assert res.status_code == 400


async def test_cannot_cancel_someone_elses_booking(client, db_session):
    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    other_token, _ = await _signup(client, "other@stateuniversity.edu")
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    res = await client.patch(
        f"/api/v1/bookings/{booking['id']}/cancel", headers=_auth_headers(other_token)
    )
    assert res.status_code == 404


async def test_status_filter_on_list_bookings(client, db_session):
    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)

    b1 = await _create_booking(client, student_token, listing_id)
    b2 = await _create_booking(client, student_token, listing_id)
    await client.patch(
        f"/api/v1/bookings/{b1['id']}", headers=_auth_headers(admin_token), json={"status": "approved"}
    )

    res = await client.get(
        "/api/v1/bookings", headers=_auth_headers(student_token), params={"status_filter": "approved"}
    )
    assert res.status_code == 200, res.text
    ids = [b["id"] for b in res.json()]
    assert ids == [b1["id"]]

    res2 = await client.get(
        "/api/v1/bookings", headers=_auth_headers(student_token), params={"status_filter": "pending"}
    )
    assert [b["id"] for b in res2.json()] == [b2["id"]]


async def test_admin_notified_by_email_on_new_booking(client, db_session, monkeypatch):
    import app.api.v1.endpoints.bookings as bookings_module

    captured = []
    monkeypatch.setattr(
        bookings_module,
        "send_booking_request_email",
        lambda admin_email, student_name, listing_title, move_in_date: captured.append(admin_email),
    )

    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    _, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)

    await _create_booking(client, student_token, listing_id)
    assert captured == ["admin@stateuniversity.edu"]


async def test_student_notified_by_email_on_decision(client, db_session, monkeypatch):
    import app.api.v1.endpoints.bookings as bookings_module

    captured = []
    monkeypatch.setattr(
        bookings_module,
        "send_booking_decision_email",
        lambda student_email, listing_title, status: captured.append((student_email, status)),
    )

    student_token, _ = await _signup(client, "student@stateuniversity.edu")
    admin_token, admin = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, admin["id"])
    listing_id = await _create_listing(db_session)
    booking = await _create_booking(client, student_token, listing_id)

    await client.patch(
        f"/api/v1/bookings/{booking['id']}", headers=_auth_headers(admin_token), json={"status": "approved"}
    )
    assert captured == [("student@stateuniversity.edu", "approved")]
