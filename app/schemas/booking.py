from datetime import date, datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, EmailStr

from app.schemas.listing import ListingRead
from app.schemas.user import UserRead


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


class BookingDecision(BaseModel):
    status: Literal["approved", "declined"]


class BookingAdminRead(BookingRead):
    """Same as BookingRead plus the requesting student's profile, for the
    admin pending-bookings review screen. Field is named `user` (not
    `student`) to match Booking.user — from_attributes maps by name."""

    user: UserRead
