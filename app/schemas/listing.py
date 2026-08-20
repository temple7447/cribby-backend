from datetime import datetime

from pydantic import BaseModel, ConfigDict


class ListingRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    title: str
    type: str
    price_per: int
    total: int
    utilities_monthly: int
    deposit: int
    walk: int
    rating: float
    reviews: int
    verified: bool
    hot: bool
    video: bool
    grad: str
    emoji: str
    amenities: list[str]
    landlord_name: str
    landlord_verified: bool
    landlord_response_rate: int
    created_at: datetime
