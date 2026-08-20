from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.crud.base import CRUDBase
from app.models.booking import Booking
from app.schemas.booking import BookingCreate


class CRUDBooking(CRUDBase[Booking, BookingCreate]):
    async def create_for_user(
        self, db: AsyncSession, obj_in: BookingCreate, user_id: int
    ) -> Booking:
        booking = Booking(
            user_id=user_id,
            listing_id=obj_in.listing_id,
            move_in_date=obj_in.move_in_date,
            lease_months=obj_in.lease_months,
            guarantor_name=obj_in.guarantor_name,
            guarantor_email=obj_in.guarantor_email,
            guarantor_status="invited" if obj_in.guarantor_email else "none",
            message_to_landlord=obj_in.message_to_landlord,
            # demo landlords approve immediately so the confirmation checklist has signal
            status="approved",
            landlord_approved=True,
        )
        db.add(booking)
        await db.commit()
        await db.refresh(booking, attribute_names=["listing"])
        return booking

    async def get_for_user(self, db: AsyncSession, id: int, user_id: int) -> Booking | None:
        stmt = (
            select(Booking)
            .where(Booking.id == id, Booking.user_id == user_id)
            .options(selectinload(Booking.listing))
        )
        result = await db.execute(stmt)
        return result.scalar_one_or_none()

    async def get_multi_for_user(self, db: AsyncSession, user_id: int) -> list[Booking]:
        stmt = (
            select(Booking)
            .where(Booking.user_id == user_id)
            .options(selectinload(Booking.listing))
            .order_by(Booking.created_at.desc())
        )
        result = await db.execute(stmt)
        return list(result.scalars().all())


booking = CRUDBooking(Booking)
