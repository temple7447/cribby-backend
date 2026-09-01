from datetime import datetime
from typing import TYPE_CHECKING

from sqlalchemy import JSON, ForeignKey, Integer, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.db.base import Base
from app.db.types import UTCDateTime

if TYPE_CHECKING:
    from app.models.user import User


class RoommateProfile(Base):
    __tablename__ = "roommate_profiles"

    id: Mapped[int] = mapped_column(primary_key=True)
    user_id: Mapped[int] = mapped_column(
        ForeignKey("users.id"), unique=True, nullable=False, index=True
    )

    name: Mapped[str] = mapped_column(String(100), nullable=False)
    age: Mapped[int] = mapped_column(Integer, nullable=False)
    school: Mapped[str] = mapped_column(String(150), nullable=False)
    year: Mapped[str] = mapped_column(
        String(30), nullable=False
    )  # Freshman | Sophomore | Junior | Senior | Grad

    budget_min: Mapped[int] = mapped_column(Integer, nullable=False)
    budget_max: Mapped[int] = mapped_column(Integer, nullable=False)
    move_in_date: Mapped[str] = mapped_column(
        String(30), nullable=False
    )  # display string, e.g. "Aug 1"

    tags: Mapped[list[str]] = mapped_column(JSON, default=list, nullable=False)
    bio: Mapped[str] = mapped_column(Text, default="", nullable=False)
    looking_for: Mapped[str] = mapped_column(Text, default="", nullable=False)
    preferred_areas: Mapped[list[str]] = mapped_column(JSON, default=list, nullable=False)

    # live | paused | under_review | removed
    visibility: Mapped[str] = mapped_column(String(20), default="live", nullable=False)

    emoji: Mapped[str] = mapped_column(String(8), nullable=False)
    grad: Mapped[str] = mapped_column(String(50), nullable=False)

    # Real cover photo for this post, independent of the poster's own profile
    # photo — optional, falls back to the emoji/gradient everywhere it's unset.
    banner_url: Mapped[str | None] = mapped_column(String(500), nullable=True)
    banner_public_id: Mapped[str | None] = mapped_column(String(255), nullable=True)

    created_at: Mapped[datetime] = mapped_column(UTCDateTime, server_default=func.now())

    user: Mapped["User"] = relationship(back_populates="roommate_profile")

    @property
    def verified(self) -> bool:
        return self.user is not None and self.user.verification_status == "verified"

    @property
    def user_avatar_url(self) -> str | None:
        return self.user.avatar_url if self.user is not None else None
