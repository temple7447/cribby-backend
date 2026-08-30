"""Schemas that stitch together other resources for admin-only views. Kept
separate from schemas/user.py to avoid a circular import (booking.py already
imports UserRead from schemas/user.py, so schemas/user.py can't import
BookingRead back)."""

from pydantic import BaseModel, ConfigDict

from app.schemas.booking import BookingRead
from app.schemas.report import ReportRead
from app.schemas.roommate import RoommateProfileRead
from app.schemas.user import UserRead


class AdminUserDetail(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    user: UserRead
    bookings: list[BookingRead]
    roommate_profile: RoommateProfileRead | None
    reports_filed: list[ReportRead]
    reports_against: list[ReportRead]
