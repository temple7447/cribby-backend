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
    from sqlalchemy import select

    user = (await db_session.execute(select(User).where(User.id == user_id))).scalar_one()
    user.is_admin = True
    await db_session.commit()


async def _verify_user(db_session, user_id: int) -> None:
    from sqlalchemy import select

    user = (await db_session.execute(select(User).where(User.id == user_id))).scalar_one()
    user.verification_status = "verified"
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


async def _create_roommate_profile(client, token: str) -> int:
    res = await client.post(
        "/api/v1/roommates",
        json={
            "name": "Zoe",
            "age": 20,
            "school": "State University",
            "year": "Sophomore",
            "budget_min": 500,
            "budget_max": 900,
            "move_in_date": "Aug 1",
        },
        headers=_auth_headers(token),
    )
    assert res.status_code == 201, res.text
    return res.json()["id"]


async def test_create_report_against_listing(client, db_session):
    token, _ = await _signup(client, "reporter@stateuniversity.edu")
    listing_id = await _create_listing(db_session)

    res = await client.post(
        "/api/v1/reports",
        json={"target_type": "listing", "target_id": listing_id, "reason": "spam"},
        headers=_auth_headers(token),
    )
    assert res.status_code == 201, res.text
    assert res.json()["status"] == "pending"


async def test_create_report_invalid_target_type_400(client):
    token, _ = await _signup(client, "reporter2@stateuniversity.edu")
    res = await client.post(
        "/api/v1/reports",
        json={"target_type": "landlord", "target_id": 1, "reason": "spam"},
        headers=_auth_headers(token),
    )
    assert res.status_code == 400


async def test_create_report_nonexistent_target_404(client):
    token, _ = await _signup(client, "reporter3@stateuniversity.edu")
    res = await client.post(
        "/api/v1/reports",
        json={"target_type": "listing", "target_id": 999999, "reason": "spam"},
        headers=_auth_headers(token),
    )
    assert res.status_code == 404


async def test_report_hides_roommate_profile_from_list(client, db_session):
    owner_token, owner = await _signup(client, "owner@stateuniversity.edu")
    await _verify_user(db_session, owner["id"])
    profile_id = await _create_roommate_profile(client, owner_token)

    listed = await client.get("/api/v1/roommates")
    assert any(p["id"] == profile_id for p in listed.json())

    reporter_token, _ = await _signup(client, "reporter4@stateuniversity.edu")
    res = await client.post(
        "/api/v1/reports",
        json={"target_type": "roommate_profile", "target_id": profile_id, "reason": "fake"},
        headers=_auth_headers(reporter_token),
    )
    assert res.status_code == 201

    listed_after = await client.get("/api/v1/roommates")
    assert all(p["id"] != profile_id for p in listed_after.json())


async def test_owner_cannot_unhide_under_review_profile_via_patch(client, db_session):
    owner_token, owner = await _signup(client, "owner2@stateuniversity.edu")
    await _verify_user(db_session, owner["id"])
    profile_id = await _create_roommate_profile(client, owner_token)

    reporter_token, _ = await _signup(client, "reporter5@stateuniversity.edu")
    await client.post(
        "/api/v1/reports",
        json={"target_type": "roommate_profile", "target_id": profile_id, "reason": "fake"},
        headers=_auth_headers(reporter_token),
    )

    res = await client.patch(
        "/api/v1/roommates/me", json={"visibility": "live"}, headers=_auth_headers(owner_token)
    )
    assert res.status_code == 403


async def test_dismiss_report_restores_live_visibility(client, db_session):
    owner_token, owner = await _signup(client, "owner3@stateuniversity.edu")
    await _verify_user(db_session, owner["id"])
    profile_id = await _create_roommate_profile(client, owner_token)

    reporter_token, reporter = await _signup(client, "reporter6@stateuniversity.edu")
    report_res = await client.post(
        "/api/v1/reports",
        json={"target_type": "roommate_profile", "target_id": profile_id, "reason": "fake"},
        headers=_auth_headers(reporter_token),
    )
    report_id = report_res.json()["id"]

    await _make_admin(db_session, reporter["id"])
    res = await client.patch(
        f"/api/v1/reports/{report_id}",
        json={"status": "dismissed"},
        headers=_auth_headers(reporter_token),
    )
    assert res.status_code == 200

    listed = await client.get("/api/v1/roommates")
    assert any(p["id"] == profile_id for p in listed.json())


async def test_resolve_report_marks_profile_removed(client, db_session):
    owner_token, owner = await _signup(client, "owner4@stateuniversity.edu")
    await _verify_user(db_session, owner["id"])
    profile_id = await _create_roommate_profile(client, owner_token)

    reporter_token, reporter = await _signup(client, "reporter7@stateuniversity.edu")
    report_res = await client.post(
        "/api/v1/reports",
        json={"target_type": "roommate_profile", "target_id": profile_id, "reason": "fake"},
        headers=_auth_headers(reporter_token),
    )
    report_id = report_res.json()["id"]

    await _make_admin(db_session, reporter["id"])
    res = await client.patch(
        f"/api/v1/reports/{report_id}",
        json={"status": "resolved"},
        headers=_auth_headers(reporter_token),
    )
    assert res.status_code == 200

    listed = await client.get("/api/v1/roommates")
    assert all(p["id"] != profile_id for p in listed.json())


async def test_list_reports_requires_admin(client, db_session):
    token, user = await _signup(client, "nonadmin@stateuniversity.edu")
    res = await client.get("/api/v1/reports", headers=_auth_headers(token))
    assert res.status_code == 403

    await _make_admin(db_session, user["id"])
    res2 = await client.get("/api/v1/reports", headers=_auth_headers(token))
    assert res2.status_code == 200


async def test_block_unblock_roundtrip(client):
    token_a, _ = await _signup(client, "blocker@stateuniversity.edu")
    _, user_b = await _signup(client, "blocked@stateuniversity.edu")

    res = await client.post(
        "/api/v1/blocks", json={"blocked_user_id": user_b["id"]}, headers=_auth_headers(token_a)
    )
    assert res.status_code == 200, res.text

    listed = await client.get("/api/v1/blocks", headers=_auth_headers(token_a))
    assert any(b["blocked_id"] == user_b["id"] for b in listed.json())

    res = await client.delete(f"/api/v1/blocks/{user_b['id']}", headers=_auth_headers(token_a))
    assert res.status_code == 204

    listed_after = await client.get("/api/v1/blocks", headers=_auth_headers(token_a))
    assert listed_after.json() == []


async def test_cannot_block_self_400(client):
    token, user = await _signup(client, "self@stateuniversity.edu")
    res = await client.post(
        "/api/v1/blocks", json={"blocked_user_id": user["id"]}, headers=_auth_headers(token)
    )
    assert res.status_code == 400


async def test_double_unblock_is_idempotent(client):
    token, _ = await _signup(client, "unblocker@stateuniversity.edu")
    res = await client.delete("/api/v1/blocks/999999", headers=_auth_headers(token))
    assert res.status_code == 204


async def test_blocked_user_cannot_message_roommate_profile_403(client, db_session):
    owner_token, owner = await _signup(client, "owner5@stateuniversity.edu")
    await _verify_user(db_session, owner["id"])
    profile_id = await _create_roommate_profile(client, owner_token)

    other_token, other = await _signup(client, "other@stateuniversity.edu")
    await _verify_user(db_session, other["id"])

    await client.post(
        "/api/v1/blocks", json={"blocked_user_id": owner["id"]}, headers=_auth_headers(other_token)
    )

    res = await client.post(
        "/api/v1/conversations",
        json={"kind": "roommate", "roommate_profile_id": profile_id},
        headers=_auth_headers(other_token),
    )
    assert res.status_code == 403


async def test_unverified_user_cannot_create_roommate_profile_403(client):
    token, _ = await _signup(client, "unverified@stateuniversity.edu")
    res = await client.post(
        "/api/v1/roommates",
        json={
            "name": "Sam",
            "age": 21,
            "school": "State University",
            "year": "Junior",
            "budget_min": 500,
            "budget_max": 900,
            "move_in_date": "Aug 1",
        },
        headers=_auth_headers(token),
    )
    assert res.status_code == 403


async def test_unverified_user_cannot_send_roommate_message(client, db_session):
    owner_token, owner = await _signup(client, "owner6@stateuniversity.edu")
    await _verify_user(db_session, owner["id"])
    profile_id = await _create_roommate_profile(client, owner_token)

    unverified_token, _ = await _signup(client, "unverified2@stateuniversity.edu")
    res = await client.post(
        "/api/v1/conversations",
        json={"kind": "roommate", "roommate_profile_id": profile_id},
        headers=_auth_headers(unverified_token),
    )
    assert res.status_code == 403
