from datetime import datetime

from pydantic import BaseModel, ConfigDict


class BlockCreate(BaseModel):
    blocked_user_id: int


class BlockedUserSummary(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    name: str
    avatar_emoji: str


class BlockRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    blocked_id: int
    created_at: datetime
    blocked_user: BlockedUserSummary
