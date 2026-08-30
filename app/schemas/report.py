from datetime import datetime

from pydantic import BaseModel, ConfigDict

REPORT_TARGET_TYPES = {"listing", "roommate_profile", "user"}
REPORT_REASONS = {"spam", "scam", "inappropriate", "fake", "other"}
REPORT_STATUSES = {"pending", "reviewing", "resolved", "dismissed"}


class ReportCreate(BaseModel):
    target_type: str
    target_id: int
    reason: str
    details: str | None = None


class ReportUpdate(BaseModel):
    status: str


class ReportRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    reporter_id: int
    target_type: str
    target_id: int
    reason: str
    details: str | None
    status: str
    created_at: datetime
    resolved_at: datetime | None
    resolved_by_id: int | None
    # Resolved server-side so a moderator UI doesn't have to make N follow-up
    # calls per report just to know what/who is actually being reported.
    reporter_email: str | None = None
    target_label: str | None = None
