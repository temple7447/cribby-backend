from datetime import datetime

from pydantic import BaseModel, ConfigDict


class ListingPhoto(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: str
    url: str
    public_id: str
    alt: str
    uploaded_at: datetime


class ListingCreate(BaseModel):
    title: str
    type: str  # Studio | Shared 4BR | 1BR | 2BR ...
    price_per: int
    total: int
    utilities_monthly: int = 45
    deposit: int
    walk: int
    rating: float = 0
    reviews: int = 0
    verified: bool = False
    hot: bool = False
    video: bool = False
    grad: str = "from-coral to-sunshine"
    emoji: str = "🏠"
    amenities: list[str] = []
    landlord_name: str = "Sandra"
    landlord_verified: bool = True
    landlord_response_rate: int = 95


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
    is_hidden: bool
    grad: str
    emoji: str
    amenities: list[str]
    photos: list[ListingPhoto]
    landlord_name: str
    landlord_verified: bool
    landlord_response_rate: int
    created_at: datetime
