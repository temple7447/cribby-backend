async def _signup(client, email, password="pw123456", name="Test"):
    res = await client.post(
        "/api/v1/auth/signup", json={"email": email, "password": password, "name": name}
    )
    assert res.status_code == 200, res.text
    body = res.json()
    return body["access_token"], body["user"]


def _auth_headers(token: str) -> dict:
    return {"Authorization": f"Bearer {token}"}


def _fake_cloudinary(monkeypatch):
    """Stub out the network calls storage.py makes so tests never hit Cloudinary."""
    import app.core.storage as storage

    monkeypatch.setattr(storage.settings, "CLOUDINARY_CLOUD_NAME", "test-cloud")

    calls = {"uploads": 0, "destroyed": []}

    def fake_upload(contents, folder=None, transformation=None):
        calls["uploads"] += 1
        return {
            "secure_url": f"https://res.cloudinary.com/test/avatar{calls['uploads']}.jpg",
            "public_id": f"avatar{calls['uploads']}",
        }

    def fake_destroy(public_id):
        calls["destroyed"].append(public_id)
        return {"result": "ok"}

    monkeypatch.setattr(storage.cloudinary.uploader, "upload", fake_upload)
    monkeypatch.setattr(storage.cloudinary.uploader, "destroy", fake_destroy)
    return calls


async def test_upload_avatar_sets_url_and_keeps_emoji_as_fallback_field(client, monkeypatch):
    _fake_cloudinary(monkeypatch)
    token, user = await _signup(client, "student@gmail.com")
    assert user["avatar_url"] is None

    res = await client.post(
        "/api/v1/users/me/avatar",
        headers=_auth_headers(token),
        files={"file": ("photo.jpg", b"fake-bytes", "image/jpeg")},
    )
    assert res.status_code == 200, res.text
    body = res.json()
    assert body["avatar_url"] == "https://res.cloudinary.com/test/avatar1.jpg"
    assert body["avatar_emoji"]  # untouched, still present as the fallback


async def test_reupload_replaces_and_deletes_old_asset(client, monkeypatch):
    calls = _fake_cloudinary(monkeypatch)
    token, _ = await _signup(client, "student2@gmail.com")

    first = await client.post(
        "/api/v1/users/me/avatar",
        headers=_auth_headers(token),
        files={"file": ("photo.jpg", b"fake-bytes-1", "image/jpeg")},
    )
    assert first.status_code == 200

    second = await client.post(
        "/api/v1/users/me/avatar",
        headers=_auth_headers(token),
        files={"file": ("photo.jpg", b"fake-bytes-2", "image/jpeg")},
    )
    assert second.status_code == 200, second.text
    assert second.json()["avatar_url"] == "https://res.cloudinary.com/test/avatar2.jpg"
    assert calls["destroyed"] == ["avatar1"]  # old asset cleaned up


async def test_delete_avatar_reverts_to_emoji_fallback(client, monkeypatch):
    calls = _fake_cloudinary(monkeypatch)
    token, _ = await _signup(client, "student3@gmail.com")

    await client.post(
        "/api/v1/users/me/avatar",
        headers=_auth_headers(token),
        files={"file": ("photo.jpg", b"fake-bytes", "image/jpeg")},
    )

    res = await client.delete("/api/v1/users/me/avatar", headers=_auth_headers(token))
    assert res.status_code == 200, res.text
    assert res.json()["avatar_url"] is None
    assert calls["destroyed"] == ["avatar1"]


async def test_upload_avatar_rejects_bad_content_type(client, monkeypatch):
    _fake_cloudinary(monkeypatch)
    token, _ = await _signup(client, "student4@gmail.com")

    res = await client.post(
        "/api/v1/users/me/avatar",
        headers=_auth_headers(token),
        files={"file": ("virus.exe", b"not-an-image", "application/octet-stream")},
    )
    assert res.status_code == 400


async def test_upload_avatar_requires_auth(client):
    res = await client.post(
        "/api/v1/users/me/avatar",
        files={"file": ("photo.jpg", b"fake-bytes", "image/jpeg")},
    )
    assert res.status_code == 401
