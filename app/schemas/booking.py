from datetime import date, datetime

from pydantic import BaseModel, ConfigDict, EmailStr

from app.schemas.listing import ListingRead


class BookingCreate(BaseModel):
    listing_id: int
    move_in_date: date
    lease_months: int = 12
    guarantor_name: str | None = None
    guarantor_email: EmailStr | None = None
    message_to_landlord: str | None = None


class BookingRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    listing_id: int
    move_in_date: date
    lease_months: int
    status: str
    guarantor_name: str | None
    guarantor_email: str | None
    guarantor_status: str
    message_to_landlord: str | None
    request_sent: bool
    landlord_approved: bool
    guarantor_signed: bool
    lease_signed: bool
    deposit_paid: bool
    keys_collected: bool
    created_at: datetime
    listing: ListingRead
