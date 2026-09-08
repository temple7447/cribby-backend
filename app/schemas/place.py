from datetime import datetime

from pydantic import BaseModel, ConfigDict


class PlaceCreate(BaseModel):
    name: str
    category: str
    address: str | None = None
    latitude: float
    longitude: float
    emoji: str = "📍"


class PlaceRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    name: str
    category: str
    address: str | None
    latitude: float
    longitude: float
    emoji: str
    # Populated only on proximity searches (near_lat / near_lng): great-circle
    # distance in km from that point to this place.
    distance_km: float | None = None
    created_at: datetime
