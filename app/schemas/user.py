from datetime import datetime

from pydantic import BaseModel, ConfigDict, EmailStr


class UserCreate(BaseModel):
    email: EmailStr
    password: str
    name: str
    school: str = "State University"


class UserLogin(BaseModel):
    email: EmailStr
    password: str


class UserRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    email: EmailStr
    name: str
    school: str
    avatar_emoji: str
    verification_status: str
    is_admin: bool
    created_at: datetime


class UserUpdate(BaseModel):
    name: str | None = None
    school: str | None = None
    avatar_emoji: str | None = None


class VerifyConfirmRequest(BaseModel):
    code: str
