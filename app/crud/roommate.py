from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.crud.base import CRUDBase
from app.models.roommate import RoommateProfile
from app.schemas.roommate import RoommateProfileCreate


class CRUDRoommate(CRUDBase[RoommateProfile, RoommateProfileCreate]):
    async def get_by_user_id(self, db: AsyncSession, user_id: int) -> RoommateProfile | None:
        result = await db.execute(select(RoommateProfile).where(RoommateProfile.user_id == user_id))
        return result.scalar_one_or_none()

    async def list_live(self, db: AsyncSession) -> list[RoommateProfile]:
        result = await db.execute(
            select(RoommateProfile).where(RoommateProfile.visibility == "live")
        )
        return list(result.scalars().all())

    async def create_for_user(
        self, db: AsyncSession, obj_in: RoommateProfileCreate, user_id: int
    ) -> RoommateProfile:
        profile = RoommateProfile(user_id=user_id, **obj_in.model_dump())
        db.add(profile)
        await db.commit()
        await db.refresh(profile)
        return profile


roommate = CRUDRoommate(RoommateProfile)
