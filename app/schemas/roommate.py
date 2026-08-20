from datetime import datetime

from pydantic import BaseModel, ConfigDict


class RoommateProfileCreate(BaseModel):
    name: str
    age: int
    school: str
    year: str
    budget_min: int
    budget_max: int
    move_in_date: str
    tags: list[str] = []
    bio: str = ""
    looking_for: str = ""
    preferred_areas: list[str] = []
    emoji: str = "🙂"
    grad: str = "from-violet to-mint"


class RoommateProfileUpdate(BaseModel):
    bio: str | None = None
    tags: list[str] | None = None
    looking_for: str | None = None
    preferred_areas: list[str] | None = None
    visibility: str | None = None


class RoommateProfileRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    user_id: int
    name: str
    age: int
    school: str
    year: str
    budget_min: int
    budget_max: int
    move_in_date: str
    tags: list[str]
    bio: str
    looking_for: str
    preferred_areas: list[str]
    verified: bool
    visibility: str
    emoji: str
    grad: str
    created_at: datetime
