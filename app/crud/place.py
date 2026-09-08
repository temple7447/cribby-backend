from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.geo import rank_by_distance
from app.crud.base import CRUDBase
from app.models.place import Place


class CRUDPlace(CRUDBase[Place, dict]):
    async def search(
        self,
        db: AsyncSession,
        *,
        category: str | None = None,
        q: str | None = None,
        near_lat: float | None = None,
        near_lng: float | None = None,
        radius_km: float | None = None,
    ) -> list[Place]:
        stmt = select(Place)
        if category and category.lower() != "any":
            stmt = stmt.where(Place.category == category.lower())
        if q:
            stmt = stmt.where(Place.name.ilike(f"%{q}%"))
        result = await db.execute(stmt)
        places = list(result.scalars().all())

        if near_lat is None or near_lng is None:
            return places

        # Proximity mode: nearest-first, stamped with distance_km, radius-filtered.
        return rank_by_distance(places, near_lat, near_lng, radius_km=radius_km)


place = CRUDPlace(Place)
