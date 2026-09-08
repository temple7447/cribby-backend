from sqlalchemy import select

from app.models.place import Place
from app.models.user import User

# University of Lagos, Akoka.
CAMPUS = (6.5158, 3.3898)


async def _signup(client, email, password="pw123456", name="Test"):
    res = await client.post(
        "/api/v1/auth/signup", json={"email": email, "password": password, "name": name}
    )
    assert res.status_code == 200, res.text
    body = res.json()
    return body["access_token"], body["user"]


def _auth(token):
    return {"Authorization": f"Bearer {token}"}


async def _make_admin(db_session, user_id):
    user = (await db_session.execute(select(User).where(User.id == user_id))).scalar_one()
    user.is_admin = True
    await db_session.commit()


def _place(db_session, name, category, lat, lng):
    obj = Place(name=name, category=category, latitude=lat, longitude=lng)
    db_session.add(obj)
    return obj


async def test_places_search_by_category_and_proximity(client, db_session):
    _place(db_session, "Far Pharmacy", "pharmacy", 6.5500, 3.4200)
    _place(db_session, "Near Pharmacy", "pharmacy", 6.5165, 3.3905)
    _place(db_session, "Akoka Hospital", "hospital", 6.5160, 3.3900)
    await db_session.commit()

    res = await client.get(
        f"/api/v1/places?category=pharmacy&near_lat={CAMPUS[0]}&near_lng={CAMPUS[1]}"
    )
    assert res.status_code == 200, res.text
    body = res.json()
    assert [p["name"] for p in body] == ["Near Pharmacy", "Far Pharmacy"]
    assert body[0]["distance_km"] < body[1]["distance_km"]


async def test_places_search_by_name(client, db_session):
    _place(db_session, "GTBank Akoka", "bank", *CAMPUS)
    _place(db_session, "Access Bank Yaba", "bank", 6.5098, 3.3790)
    await db_session.commit()

    res = await client.get("/api/v1/places?q=gtbank")
    assert [p["name"] for p in res.json()] == ["GTBank Akoka"]


async def test_places_radius_filter(client, db_session):
    _place(db_session, "Inside", "market", 6.5165, 3.3905)
    _place(db_session, "Outside", "market", 6.7000, 3.5000)
    await db_session.commit()

    res = await client.get(
        f"/api/v1/places?near_lat={CAMPUS[0]}&near_lng={CAMPUS[1]}&radius_km=3"
    )
    assert [p["name"] for p in res.json()] == ["Inside"]


async def test_places_without_proximity_returns_all_unsorted(client, db_session):
    _place(db_session, "A", "gym", *CAMPUS)
    _place(db_session, "B", "gym", 6.6, 3.5)
    await db_session.commit()

    res = await client.get("/api/v1/places")
    body = res.json()
    assert {p["name"] for p in body} == {"A", "B"}
    assert all(p["distance_km"] is None for p in body)


async def test_create_place_requires_admin(client, db_session):
    token, _ = await _signup(client, "plain-place@gmail.com")
    res = await client.post(
        "/api/v1/places",
        headers=_auth(token),
        json={"name": "X", "category": "Pharmacy", "latitude": 6.5, "longitude": 3.3},
    )
    assert res.status_code == 403


async def test_create_place_lowercases_category(client, db_session):
    token, user = await _signup(client, "admin-place@gmail.com")
    await _make_admin(db_session, user["id"])
    res = await client.post(
        "/api/v1/places",
        headers=_auth(token),
        json={"name": "MedPlus", "category": "Pharmacy", "latitude": 6.5, "longitude": 3.3},
    )
    assert res.status_code == 201, res.text
    assert res.json()["category"] == "pharmacy"
