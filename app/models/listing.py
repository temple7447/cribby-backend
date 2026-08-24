from datetime import datetime
from typing import TYPE_CHECKING

from sqlalchemy import JSON, Boolean, Float, Integer, String, func
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.db.base import Base
from app.db.types import UTCDateTime

if TYPE_CHECKING:
    from app.models.booking import Booking
    from app.models.favorite import Favorite


class Listing(Base):
    __tablename__ = "listings"

    id: Mapped[int] = mapped_column(primary_key=True)
    title: Mapped[str] = mapped_column(String(200), nullable=False)
    type: Mapped[str] = mapped_column(
        String(50), nullable=False
    )  # Studio | Shared 4BR | 1BR | 2BR ...

    price_per: Mapped[int] = mapped_column(
        Integer, nullable=False
    )  # per-person monthly, headline price
    total: Mapped[int] = mapped_column(Integer, nullable=False)  # total monthly rent
    utilities_monthly: Mapped[int] = mapped_column(Integer, default=45, nullable=False)
    deposit: Mapped[int] = mapped_column(Integer, nullable=False)

    walk: Mapped[int] = mapped_column(Integer, nullable=False)  # walk minutes to campus
    rating: Mapped[float] = mapped_column(Float, default=0, nullable=False)
    reviews: Mapped[int] = mapped_column(Integer, default=0, nullable=False)

    verified: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    hot: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    video: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    is_hidden: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)

    grad: Mapped[str] = mapped_column(
        String(50), nullable=False
    )  # gradient key, e.g. "from-coral to-sunshine"
    emoji: Mapped[str] = mapped_column(String(8), nullable=False)
    amenities: Mapped[list[str]] = mapped_column(JSON, default=list, nullable=False)

    landlord_name: Mapped[str] = mapped_column(String(100), default="Sandra", nullable=False)
    landlord_verified: Mapped[bool] = mapped_column(Boolean, default=True, nullable=False)
    landlord_response_rate: Mapped[int] = mapped_column(Integer, default=95, nullable=False)

    created_at: Mapped[datetime] = mapped_column(UTCDateTime, server_default=func.now())

    bookings: Mapped[list["Booking"]] = relationship(back_populates="listing")
    favorited_by: Mapped[list["Favorite"]] = relationship(back_populates="listing")
