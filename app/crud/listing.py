from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.geo import rank_by_distance
from app.crud.base import CRUDBase
from app.models.listing import Listing


class CRUDListing(CRUDBase[Listing, dict]):
    async def search(
        self,
        db: AsyncSession,
        *,
        max_price: int | None = None,
        max_walk: int | None = None,
        room_type: str | None = None,
        q: str | None = None,
        near_lat: float | None = None,
        near_lng: float | None = None,
        radius_km: float | None = None,
    ) -> list[Listing]:
        stmt = select(Listing).where(Listing.is_hidden == False)  # noqa: E712
        if max_price is not None:
            stmt = stmt.where(Listing.price_per <= max_price)
        if max_walk is not None:
            stmt = stmt.where(Listing.walk <= max_walk)
        if room_type and room_type.lower() != "any":
            stmt = stmt.where(Listing.type.ilike(f"%{room_type}%"))
        if q:
            stmt = stmt.where(Listing.title.ilike(f"%{q}%"))
        result = await db.execute(stmt)
        listings = list(result.scalars().all())

        if near_lat is None or near_lng is None:
            return listings

        # Proximity mode: sort nearest-first, stamp distance_km, drop listings
        # with no coordinates (and anything past radius_km, if given).
        return rank_by_distance(listings, near_lat, near_lng, radius_km=radius_km)


listing = CRUDListing(Listing)
