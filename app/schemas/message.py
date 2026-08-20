from datetime import datetime

from pydantic import BaseModel, ConfigDict


class MessageCreate(BaseModel):
    text: str


class ConversationCreate(BaseModel):
    kind: str  # landlord | roommate
    listing_id: int | None = None
    roommate_profile_id: int | None = None


class MessageRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    sender: str
    text: str
    created_at: datetime


class ConversationRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    kind: str
    listing_id: int | None
    roommate_profile_id: int | None
    title: str
    subtitle: str
    emoji: str
    grad: str
    created_at: datetime
    messages: list[MessageRead] = []
