"""One-off / idempotent: populate the `places` table and backfill listing
coordinates, so proximity search (`?near_lat&near_lng`) works.

Safe to run against any environment, including production — it only inserts
places that don't already exist (matched by name) and only sets coordinates
on listings that don't have them yet.

Run locally:            uv run python scripts/seed_places.py
Run against prod:       DATABASE_URL="postgresql+asyncpg://..." uv run python scripts/seed_places.py
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from sqlalchemy import select

from app.db.session import AsyncSessionLocal
from app.models.listing import Listing
from app.models.place import Place
from scripts.seed import LISTINGS, PLACES


async def seed_places() -> None:
    async with AsyncSessionLocal() as db:
        # --- places: insert any that aren't already there (match on name) ---
        existing_names = set(
            (await db.execute(select(Place.name))).scalars().all()
        )
        added = 0
        for p in PLACES:
            if p["name"] in existing_names:
                continue
            db.add(Place(**p))
            added += 1
        print(f"places: +{added} added, {len(existing_names)} already present")

        # --- listings: backfill coordinates by title, only where missing ---
        coords_by_title = {
            item["title"]: (item.get("latitude"), item.get("longitude"))
            for item in LISTINGS
        }
        result = await db.execute(select(Listing))
        backfilled = 0
        for listing in result.scalars().all():
            if listing.latitude is not None and listing.longitude is not None:
                continue
            lat, lng = coords_by_title.get(listing.title, (None, None))
            if lat is None or lng is None:
                print(f"listing: skip (no mapped coords): {listing.title}")
                continue
            listing.latitude = lat
            listing.longitude = lng
            db.add(listing)
            backfilled += 1
        print(f"listings: {backfilled} coordinates backfilled")

        await db.commit()


if __name__ == "__main__":
    asyncio.run(seed_places())
