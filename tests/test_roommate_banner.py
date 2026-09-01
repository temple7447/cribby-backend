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


async def _verify_user(db_session, user_id: int) -> None:
    user = (await db_session.execute(select(User).where(User.id == user_id))).scalar_one()
    user.verification_status = "verified"
    await db_session.commit()


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


def _fake_cloudinary(monkeypatch):
    import app.core.storage as storage

    monkeypatch.setattr(storage.settings, "CLOUDINARY_CLOUD_NAME", "test-cloud")

    calls = {"uploads": 0, "destroyed": []}

    def fake_upload(contents, folder=None, transformation=None):
        calls["uploads"] += 1
        return {
            "secure_url": f"https://res.cloudinary.com/test/banner{calls['uploads']}.jpg",
            "public_id": f"banner{calls['uploads']}",
        }

    def fake_destroy(public_id):
        calls["destroyed"].append(public_id)
        return {"result": "ok"}

    monkeypatch.setattr(storage.cloudinary.uploader, "upload", fake_upload)
    monkeypatch.setattr(storage.cloudinary.uploader, "destroy", fake_destroy)
    return calls


async def test_upload_banner_requires_a_profile(client, db_session, monkeypatch):
    _fake_cloudinary(monkeypatch)
    token, user = await _signup(client, "nopro@gmail.com")
    await _verify_user(db_session, user["id"])

    res = await client.post(
        "/api/v1/roommates/me/banner",
        headers=_auth_headers(token),
        files={"file": ("banner.jpg", b"fake-bytes", "image/jpeg")},
    )
    assert res.status_code == 404


async def test_upload_and_delete_banner(client, db_session, monkeypatch):
    calls = _fake_cloudinary(monkeypatch)
    token, user = await _signup(client, "banner1@gmail.com")
    await _verify_user(db_session, user["id"])
    await _create_roommate_profile(client, token)

    res = await client.post(
        "/api/v1/roommates/me/banner",
        headers=_auth_headers(token),
        files={"file": ("banner.jpg", b"fake-bytes", "image/jpeg")},
    )
    assert res.status_code == 200, res.text
    assert res.json()["banner_url"] == "https://res.cloudinary.com/test/banner1.jpg"

    res2 = await client.delete("/api/v1/roommates/me/banner", headers=_auth_headers(token))
    assert res2.status_code == 200, res2.text
    assert res2.json()["banner_url"] is None
    assert calls["destroyed"] == ["banner1"]


async def test_reupload_banner_deletes_old_asset(client, db_session, monkeypatch):
    calls = _fake_cloudinary(monkeypatch)
    token, user = await _signup(client, "banner2@gmail.com")
    await _verify_user(db_session, user["id"])
    await _create_roommate_profile(client, token)

    await client.post(
        "/api/v1/roommates/me/banner",
        headers=_auth_headers(token),
        files={"file": ("banner.jpg", b"fake-bytes-1", "image/jpeg")},
    )
    second = await client.post(
        "/api/v1/roommates/me/banner",
        headers=_auth_headers(token),
        files={"file": ("banner.jpg", b"fake-bytes-2", "image/jpeg")},
    )
    assert second.status_code == 200
    assert second.json()["banner_url"] == "https://res.cloudinary.com/test/banner2.jpg"
    assert calls["destroyed"] == ["banner1"]


async def test_roommate_read_includes_user_avatar_url(client, db_session):
    token, user = await _signup(client, "avatarcheck@gmail.com")
    await _verify_user(db_session, user["id"])
    profile_id = await _create_roommate_profile(client, token)

    res = await client.get(f"/api/v1/roommates/{profile_id}")
    assert res.status_code == 200
    assert "user_avatar_url" in res.json()
    assert res.json()["user_avatar_url"] is None  # no avatar set yet
