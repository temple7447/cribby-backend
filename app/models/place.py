from datetime import datetime

from sqlalchemy import Float, String, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base
from app.db.types import UTCDateTime


class Place(Base):
    """A point of interest students search for near a listing or campus —
    hospitals, pharmacies, banks/ATMs, markets, bus stops, gyms, places of
    worship, etc. Queried by distance from a given lat/lng.

    This is our own curated table so search works with zero external
    dependencies. The /places search endpoint has the same shape a maps
    Places API would, so it can be backed by Google/OSM later without the
    client changing.
    """

    __tablename__ = "places"

    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(200), nullable=False)
    # Free-text but conventionally one of: hospital, clinic, pharmacy, bank, atm,
    # market, supermarket, restaurant, bus_stop, gym, laundry, police, worship.
    category: Mapped[str] = mapped_column(String(50), nullable=False, index=True)
    address: Mapped[str | None] = mapped_column(String(300), nullable=True)
    latitude: Mapped[float] = mapped_column(Float, nullable=False)
    longitude: Mapped[float] = mapped_column(Float, nullable=False)
    emoji: Mapped[str] = mapped_column(String(8), default="📍", nullable=False)

    created_at: Mapped[datetime] = mapped_column(UTCDateTime, server_default=func.now())
