from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

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
    ) -> list[Listing]:
        stmt = select(Listing)
        if max_price is not None:
            stmt = stmt.where(Listing.price_per <= max_price)
        if max_walk is not None:
            stmt = stmt.where(Listing.walk <= max_walk)
        if room_type and room_type.lower() != "any":
            stmt = stmt.where(Listing.type.ilike(f"%{room_type}%"))
        if q:
            stmt = stmt.where(Listing.title.ilike(f"%{q}%"))
        result = await db.execute(stmt)
        return list(result.scalars().all())


listing = CRUDListing(Listing)
