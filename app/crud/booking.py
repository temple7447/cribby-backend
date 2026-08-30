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
            status="pending",
            landlord_approved=False,
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

    async def get_multi_for_user(
        self, db: AsyncSession, user_id: int, status: str | None = None
    ) -> list[Booking]:
        stmt = (
            select(Booking)
            .where(Booking.user_id == user_id)
            .options(selectinload(Booking.listing))
            .order_by(Booking.created_at.desc())
        )
        if status is not None:
            stmt = stmt.where(Booking.status == status)
        result = await db.execute(stmt)
        return list(result.scalars().all())

    async def get_with_relations(self, db: AsyncSession, id: int) -> Booking | None:
        """Admin lookup — not scoped to a particular user, unlike get_for_user."""
        stmt = (
            select(Booking)
            .where(Booking.id == id)
            .options(selectinload(Booking.listing), selectinload(Booking.user))
        )
        result = await db.execute(stmt)
        return result.scalar_one_or_none()

    async def list_pending(self, db: AsyncSession) -> list[Booking]:
        stmt = (
            select(Booking)
            .where(Booking.status == "pending")
            .options(selectinload(Booking.listing), selectinload(Booking.user))
            .order_by(Booking.created_at.asc())
        )
        result = await db.execute(stmt)
        return list(result.scalars().all())

    async def decide(self, db: AsyncSession, booking: Booking, new_status: str) -> Booking:
        booking.status = new_status
        booking.landlord_approved = new_status == "approved"
        db.add(booking)
        await db.commit()
        await db.refresh(booking, attribute_names=["listing", "user"])
        return booking

    async def cancel(self, db: AsyncSession, booking: Booking) -> Booking:
        booking.status = "canceled"
        db.add(booking)
        await db.commit()
        await db.refresh(booking, attribute_names=["listing"])
        return booking


booking = CRUDBooking(Booking)
