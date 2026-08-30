"""One-off: attach a real photo to each demo listing via Cloudinary, so the
seeded data shows an actual photo instead of the gradient+emoji placeholder.

Cloudinary fetches the remote URL itself (no local download needed) and
re-hosts it under our cloud, matching how admin-uploaded photos are stored.

Run with: uv run python scripts/seed_photos.py
"""

import asyncio
import sys
import uuid
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import cloudinary
import cloudinary.uploader
from sqlalchemy import select
from sqlalchemy.orm.attributes import flag_modified

from app.core.config import settings
from app.db.session import AsyncSessionLocal
from app.models.listing import Listing

# Real, license-free (Unsplash) stock photos picked to roughly match each
# listing's vibe — not the actual property, just a real interior/exterior
# photo instead of an emoji.
PHOTOS_BY_TITLE = {
    "Sunny Studio on Maple St": "https://images.unsplash.com/photo-1522708323590-d24dbb6b0267?w=1600&q=80",
    "The Foundry House": "https://images.unsplash.com/photo-1600585154340-be6161a56a0c?w=1600&q=80",
    "Corner 1BR with City View": "https://images.unsplash.com/photo-1560448204-e02f11c3d0e2?w=1600&q=80",
    "Cozy 2BR + Balcony": "https://images.unsplash.com/photo-1493809842364-78817add7ffb?w=1600&q=80",
    "Garden Studio": "https://images.unsplash.com/photo-1502672260266-1c1ef2d93688?w=1600&q=80",
    "Loft near North Gate": "https://images.unsplash.com/photo-1512918728675-ed5a9ecdebfd?w=1600&q=80",
}


async def seed_photos() -> None:
    if not settings.CLOUDINARY_CLOUD_NAME:
        print("CLOUDINARY_CLOUD_NAME not set — nothing to do.")
        return

    cloudinary.config(
        cloud_name=settings.CLOUDINARY_CLOUD_NAME,
        api_key=settings.CLOUDINARY_API_KEY,
        api_secret=settings.CLOUDINARY_API_SECRET,
        secure=True,
    )

    async with AsyncSessionLocal() as db:
        result = await db.execute(select(Listing))
        listings = result.scalars().all()

        for listing in listings:
            source_url = PHOTOS_BY_TITLE.get(listing.title)
            if source_url is None:
                print(f"skip (no mapped photo): {listing.title}")
                continue
            if listing.photos:
                print(f"skip (already has photos): {listing.title}")
                continue

            uploaded = cloudinary.uploader.upload(
                source_url,
                folder=f"cribby/listings/{listing.id}",
                transformation=[
                    {"width": 1600, "height": 1600, "crop": "limit"},
                    {"quality": "auto", "fetch_format": "auto"},
                ],
            )
            photo = {
                "id": str(uuid.uuid4()),
                "url": uploaded["secure_url"],
                "public_id": uploaded["public_id"],
                "alt": listing.title,
                "uploaded_at": datetime.now(timezone.utc).isoformat(),
            }
            listing.photos = [photo]
            flag_modified(listing, "photos")
            db.add(listing)
            print(f"uploaded: {listing.title} -> {uploaded['secure_url']}")

        await db.commit()


if __name__ == "__main__":
    asyncio.run(seed_photos())
