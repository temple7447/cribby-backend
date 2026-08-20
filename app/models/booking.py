from datetime import date, datetime
from typing import TYPE_CHECKING

from sqlalchemy import Boolean, Date, ForeignKey, Integer, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.db.base import Base
from app.db.types import UTCDateTime

if TYPE_CHECKING:
    from app.models.listing import Listing
    from app.models.user import User


class Booking(Base):
    __tablename__ = "bookings"

    id: Mapped[int] = mapped_column(primary_key=True)
    user_id: Mapped[int] = mapped_column(ForeignKey("users.id"), nullable=False, index=True)
    listing_id: Mapped[int] = mapped_column(ForeignKey("listings.id"), nullable=False, index=True)

    move_in_date: Mapped[date] = mapped_column(Date, nullable=False)
    lease_months: Mapped[int] = mapped_column(Integer, default=12, nullable=False)
    # pending | approved | declined
    status: Mapped[str] = mapped_column(String(20), default="pending", nullable=False)

    guarantor_name: Mapped[str | None] = mapped_column(String(100), nullable=True)
    guarantor_email: Mapped[str | None] = mapped_column(String(255), nullable=True)
    # none | invited | signed
    guarantor_status: Mapped[str] = mapped_column(String(20), default="none", nullable=False)

    message_to_landlord: Mapped[str | None] = mapped_column(Text, nullable=True)

    # move-in checklist — each step tracked independently so the UI can show
    # who's blocking progress, matching the design doc's ChecklistTracker
    request_sent: Mapped[bool] = mapped_column(Boolean, default=True, nullable=False)
    landlord_approved: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    guarantor_signed: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    lease_signed: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    deposit_paid: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    keys_collected: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)

    created_at: Mapped[datetime] = mapped_column(UTCDateTime, server_default=func.now())

    user: Mapped["User"] = relationship(back_populates="bookings")
    listing: Mapped["Listing"] = relationship(back_populates="bookings")
