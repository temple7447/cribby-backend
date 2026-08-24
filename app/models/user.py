from datetime import datetime
from typing import TYPE_CHECKING

from sqlalchemy import Boolean, String, func
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.db.base import Base
from app.db.types import UTCDateTime

if TYPE_CHECKING:
    from app.models.booking import Booking
    from app.models.favorite import Favorite
    from app.models.roommate import RoommateProfile


class User(Base):
    __tablename__ = "users"

    id: Mapped[int] = mapped_column(primary_key=True)
    email: Mapped[str] = mapped_column(String(255), unique=True, index=True, nullable=False)
    hashed_password: Mapped[str] = mapped_column(String(255), nullable=False)
    name: Mapped[str] = mapped_column(String(100), nullable=False)
    school: Mapped[str] = mapped_column(String(150), default="State University", nullable=False)
    avatar_emoji: Mapped[str] = mapped_column(String(8), default="🎓", nullable=False)
    # "unverified" | "verified"
    verification_status: Mapped[str] = mapped_column(
        String(20), default="unverified", nullable=False
    )
    # sha256 hex digest of the current 6-digit code; cleared once verified
    verification_code_hash: Mapped[str | None] = mapped_column(String(64), nullable=True)
    verification_code_expires_at: Mapped[datetime | None] = mapped_column(
        UTCDateTime, nullable=True
    )
    verification_code_last_sent_at: Mapped[datetime | None] = mapped_column(
        UTCDateTime, nullable=True
    )
    is_admin: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    created_at: Mapped[datetime] = mapped_column(UTCDateTime, server_default=func.now())

    bookings: Mapped[list["Booking"]] = relationship(back_populates="user")
    favorites: Mapped[list["Favorite"]] = relationship(back_populates="user")
    roommate_profile: Mapped["RoommateProfile | None"] = relationship(back_populates="user")
