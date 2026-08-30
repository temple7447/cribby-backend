from sqlalchemy import select

from app.models.block import Block
from app.models.booking import Booking
from app.models.favorite import Favorite
from app.models.listing import Listing
from app.models.message import Conversation, Message
from app.models.roommate import RoommateProfile
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


async def test_wrong_password_rejected_account_survives(client, db_session):
    token, user = await _signup(client, "keepme@gmail.com")
    res = await client.request(
        "DELETE", "/api/v1/users/me", headers=_auth_headers(token), json={"password": "wrong"}
    )
    assert res.status_code == 401

    still_there = (
        await db_session.execute(select(User).where(User.id == user["id"]))
    ).scalar_one_or_none()
    assert still_there is not None


async def test_delete_removes_account_and_blocks_further_login(client):
    token, _ = await _signup(client, "bye@gmail.com", password="pw123456")
    res = await client.request(
        "DELETE",
        "/api/v1/users/me",
        headers=_auth_headers(token),
        json={"password": "pw123456"},
    )
    assert res.status_code == 204

    login = await client.post(
        "/api/v1/auth/login", json={"email": "bye@gmail.com", "password": "pw123456"}
    )
    assert login.status_code == 401


async def test_delete_cascades_owned_data(client, db_session):
    token, user = await _signup(client, "cascade@gmail.com", password="pw123456")
    listing_id = await _create_listing(db_session)

    booking_res = await client.post(
        "/api/v1/bookings",
        headers=_auth_headers(token),
        json={"listing_id": listing_id, "move_in_date": "2026-09-01", "lease_months": 12},
    )
    assert booking_res.status_code == 201, booking_res.text

    fav_res = await client.put(f"/api/v1/favorites/{listing_id}", headers=_auth_headers(token))
    assert fav_res.status_code == 204

    profile = RoommateProfile(
        user_id=user["id"],
        name="Test",
        age=20,
        school="State University",
        year="Junior",
        budget_min=400,
        budget_max=600,
        move_in_date="Aug 1",
        emoji="🎨",
        grad="from-coral to-sunshine",
    )
    db_session.add(profile)
    await db_session.commit()
    await db_session.refresh(profile)

    convo = Conversation(
        user_id=user["id"],
        kind="landlord",
        listing_id=listing_id,
        title="Sandra",
        emoji="👩‍💼",
        grad="from-coral to-sunshine",
    )
    db_session.add(convo)
    await db_session.commit()
    await db_session.refresh(convo)
    db_session.add(Message(conversation_id=convo.id, sender="me", text="hi"))
    await db_session.commit()

    # a second user who blocked/was-blocked-by, and who messaged the first
    # user's roommate profile — their conversation should survive, unlinked.
    other_token, other_user = await _signup(client, "other@gmail.com", password="pw123456")
    block_res = await client.post(
        "/api/v1/blocks", headers=_auth_headers(token), json={"blocked_user_id": other_user["id"]}
    )
    assert block_res.status_code == 200, block_res.text

    other_convo = Conversation(
        user_id=other_user["id"],
        kind="roommate",
        roommate_profile_id=profile.id,
        title="Test",
        emoji="🎨",
        grad="from-coral to-sunshine",
    )
    db_session.add(other_convo)
    await db_session.commit()
    await db_session.refresh(other_convo)
    other_convo_id = other_convo.id

    res = await client.request(
        "DELETE",
        "/api/v1/users/me",
        headers=_auth_headers(token),
        json={"password": "pw123456"},
    )
    assert res.status_code == 204

    assert (await db_session.execute(select(Booking))).scalars().all() == []
    assert (await db_session.execute(select(Favorite))).scalars().all() == []
    assert (
        await db_session.execute(select(RoommateProfile).where(RoommateProfile.id == profile.id))
    ).scalar_one_or_none() is None
    assert (
        await db_session.execute(select(Conversation).where(Conversation.id == convo.id))
    ).scalar_one_or_none() is None
    assert (
        await db_session.execute(select(Message).where(Message.conversation_id == convo.id))
    ).scalars().all() == []
    assert (await db_session.execute(select(Block))).scalars().all() == []

    survived = (
        await db_session.execute(select(Conversation).where(Conversation.id == other_convo_id))
    ).scalar_one()
    assert survived.roommate_profile_id is None

    assert (
        await db_session.execute(select(User).where(User.email == "cascade@gmail.com"))
    ).scalar_one_or_none() is None
