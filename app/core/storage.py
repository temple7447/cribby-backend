"""Listing photo storage via Cloudinary.

Cloudinary handles resizing/optimization/CDN delivery itself — we just upload
the raw bytes with a couple of transformation params and it does the rest, no
local image processing needed.
"""

import logging

import cloudinary
import cloudinary.uploader
from fastapi import HTTPException, UploadFile, status

from app.core.config import settings

logger = logging.getLogger(__name__)

_configured = False


def _ensure_configured() -> None:
    global _configured
    if not settings.CLOUDINARY_CLOUD_NAME:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="Photo storage is not configured on this environment",
        )
    if not _configured:
        cloudinary.config(
            cloud_name=settings.CLOUDINARY_CLOUD_NAME,
            api_key=settings.CLOUDINARY_API_KEY,
            api_secret=settings.CLOUDINARY_API_SECRET,
            secure=True,
        )
        _configured = True


MAX_UPLOAD_BYTES = 8 * 1024 * 1024  # 8MB
ALLOWED_CONTENT_TYPES = {"image/jpeg", "image/png", "image/webp", "image/heic"}


async def _read_validated(file: UploadFile) -> bytes:
    if file.content_type not in ALLOWED_CONTENT_TYPES:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Unsupported image type: {file.content_type}",
        )
    contents = await file.read()
    if len(contents) > MAX_UPLOAD_BYTES:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="Image must be under 8MB",
        )
    return contents


async def upload_listing_photo(file: UploadFile, listing_id: int) -> dict:
    """Validates and uploads one photo, returning {url, public_id}."""
    _ensure_configured()
    contents = await _read_validated(file)
    result = cloudinary.uploader.upload(
        contents,
        folder=f"cribby/listings/{listing_id}",
        transformation=[
            {"width": 1600, "height": 1600, "crop": "limit"},
            {"quality": "auto", "fetch_format": "auto"},
        ],
    )
    return {"url": result["secure_url"], "public_id": result["public_id"]}


def delete_listing_photo(public_id: str) -> None:
    _ensure_configured()
    cloudinary.uploader.destroy(public_id)


async def upload_avatar_photo(file: UploadFile, user_id: int) -> dict:
    """Validates and uploads a profile photo, returning {url, public_id}.
    Cropped square and face-aware where Cloudinary can detect one, since
    avatars render as circles everywhere in the app."""
    _ensure_configured()
    contents = await _read_validated(file)
    result = cloudinary.uploader.upload(
        contents,
        folder=f"cribby/avatars/{user_id}",
        transformation=[
            {"width": 512, "height": 512, "crop": "fill", "gravity": "face"},
            {"quality": "auto", "fetch_format": "auto"},
        ],
    )
    return {"url": result["secure_url"], "public_id": result["public_id"]}


def delete_avatar_photo(public_id: str) -> None:
    _ensure_configured()
    cloudinary.uploader.destroy(public_id)


async def upload_roommate_banner(file: UploadFile, roommate_profile_id: int) -> dict:
    """Validates and uploads a roommate post's cover photo, returning
    {url, public_id}. Wide crop (not square) — this renders as a full-width
    banner, not a circle."""
    _ensure_configured()
    contents = await _read_validated(file)
    result = cloudinary.uploader.upload(
        contents,
        folder=f"cribby/roommate-banners/{roommate_profile_id}",
        transformation=[
            {"width": 1200, "height": 400, "crop": "fill", "gravity": "auto"},
            {"quality": "auto", "fetch_format": "auto"},
        ],
    )
    return {"url": result["secure_url"], "public_id": result["public_id"]}


def delete_roommate_banner(public_id: str) -> None:
    _ensure_configured()
    cloudinary.uploader.destroy(public_id)
