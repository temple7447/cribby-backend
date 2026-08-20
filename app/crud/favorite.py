from sqlalchemy import delete, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.models.favorite import Favorite


async def list_for_user(db: AsyncSession, user_id: int) -> list[Favorite]:
    stmt = (
        select(Favorite).where(Favorite.user_id == user_id).options(selectinload(Favorite.listing))
    )
    result = await db.execute(stmt)
    return list(result.scalars().all())


async def add(db: AsyncSession, user_id: int, listing_id: int) -> Favorite:
    existing = await db.execute(
        select(Favorite).where(Favorite.user_id == user_id, Favorite.listing_id == listing_id)
    )
    favorite = existing.scalar_one_or_none()
    if favorite is not None:
        return favorite
    favorite = Favorite(user_id=user_id, listing_id=listing_id)
    db.add(favorite)
    await db.commit()
    await db.refresh(favorite, attribute_names=["listing"])
    return favorite


async def remove(db: AsyncSession, user_id: int, listing_id: int) -> None:
    await db.execute(
        delete(Favorite).where(Favorite.user_id == user_id, Favorite.listing_id == listing_id)
    )
    await db.commit()
