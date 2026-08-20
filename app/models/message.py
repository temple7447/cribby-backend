from datetime import datetime
from typing import TYPE_CHECKING

from sqlalchemy import ForeignKey, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.db.base import Base
from app.db.types import UTCDateTime

if TYPE_CHECKING:
    from app.models.listing import Listing
    from app.models.roommate import RoommateProfile


class Conversation(Base):
    __tablename__ = "conversations"

    id: Mapped[int] = mapped_column(primary_key=True)
    user_id: Mapped[int] = mapped_column(ForeignKey("users.id"), nullable=False, index=True)

    # landlord | roommate
    kind: Mapped[str] = mapped_column(String(20), nullable=False)
    listing_id: Mapped[int | None] = mapped_column(ForeignKey("listings.id"), nullable=True)
    roommate_profile_id: Mapped[int | None] = mapped_column(
        ForeignKey("roommate_profiles.id"), nullable=True
    )

    title: Mapped[str] = mapped_column(
        String(150), nullable=False
    )  # display name, e.g. "Sandra" or "Zoe"
    subtitle: Mapped[str] = mapped_column(String(200), default="", nullable=False)
    emoji: Mapped[str] = mapped_column(String(8), nullable=False)
    grad: Mapped[str] = mapped_column(String(50), nullable=False)

    created_at: Mapped[datetime] = mapped_column(UTCDateTime, server_default=func.now())

    listing: Mapped["Listing | None"] = relationship()
    roommate_profile: Mapped["RoommateProfile | None"] = relationship()
    messages: Mapped[list["Message"]] = relationship(
        back_populates="conversation", order_by="Message.created_at"
    )


class Message(Base):
    __tablename__ = "messages"

    id: Mapped[int] = mapped_column(primary_key=True)
    conversation_id: Mapped[int] = mapped_column(
        ForeignKey("conversations.id"), nullable=False, index=True
    )

    # "me" | "them" | "system" — system messages render booking/roommate events
    sender: Mapped[str] = mapped_column(String(10), nullable=False)
    text: Mapped[str] = mapped_column(Text, nullable=False)
    read: Mapped[bool] = mapped_column(default=True, nullable=False)

    created_at: Mapped[datetime] = mapped_column(UTCDateTime, server_default=func.now())

    conversation: Mapped["Conversation"] = relationship(back_populates="messages")
