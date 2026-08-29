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


def _fake_cloudinary(monkeypatch):
    """Stub out the network calls storage.py makes so tests never hit Cloudinary."""
    import app.core.storage as storage

    monkeypatch.setattr(storage.settings, "CLOUDINARY_CLOUD_NAME", "test-cloud")

    def fake_upload(contents, folder=None, transformation=None):
        return {"secure_url": "https://res.cloudinary.com/test/fake.jpg", "public_id": "fake123"}

    def fake_destroy(public_id):
        return {"result": "ok"}

    monkeypatch.setattr(storage.cloudinary.uploader, "upload", fake_upload)
    monkeypatch.setattr(storage.cloudinary.uploader, "destroy", fake_destroy)


async def test_upload_photo_requires_admin(client, db_session, monkeypatch):
    _fake_cloudinary(monkeypatch)
    token, user = await _signup(client, "student@stateuniversity.edu")
    listing_id = await _create_listing(db_session)

    res = await client.post(
        f"/api/v1/listings/{listing_id}/photos",
        headers=_auth_headers(token),
        files={"file": ("photo.jpg", b"fake-bytes", "image/jpeg")},
    )
    assert res.status_code == 403


async def test_upload_and_delete_photo(client, db_session, monkeypatch):
    _fake_cloudinary(monkeypatch)
    token, user = await _signup(client, "admin@stateuniversity.edu")
    await _make_admin(db_session, user["id"])
    listing_id = await _create_listing(db_session)

    res = await client.post(
        f"/api/v1/listings/{listing_id}/photos",
        headers=_auth_headers(token),
        files={"file": ("photo.jpg", b"fake-bytes", "image/jpeg")},
        data={"alt": "Living room"},
    )
    assert res.status_code == 200, res.text
    body = res.json()
    assert len(body["photos"]) == 1
    photo = body["photos"][0]
    assert photo["url"] == "https://res.cloudinary.com/test/fake.jpg"
    assert photo["alt"] == "Living room"

    res = await client.delete(
        f"/api/v1/listings/{listing_id}/photos/{photo['id']}",
        headers=_auth_headers(token),
    )
    assert res.status_code == 200, res.text
    assert res.json()["photos"] == []


async def test_upload_rejects_bad_content_type(client, db_session, monkeypatch):
    _fake_cloudinary(monkeypatch)
    token, user = await _signup(client, "admin2@stateuniversity.edu")
    await _make_admin(db_session, user["id"])
    listing_id = await _create_listing(db_session)

    res = await client.post(
        f"/api/v1/listings/{listing_id}/photos",
        headers=_auth_headers(token),
        files={"file": ("virus.exe", b"not-an-image", "application/octet-stream")},
    )
    assert res.status_code == 400
