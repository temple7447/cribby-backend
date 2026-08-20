from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.models.listing import Listing
from app.models.message import Conversation, Message
from app.models.roommate import RoommateProfile


async def list_conversations_for_user(db: AsyncSession, user_id: int) -> list[Conversation]:
    stmt = (
        select(Conversation)
        .where(Conversation.user_id == user_id)
        .options(selectinload(Conversation.messages))
        .order_by(Conversation.created_at.desc())
    )
    result = await db.execute(stmt)
    return list(result.scalars().all())


async def get_conversation_for_user(db: AsyncSession, id: int, user_id: int) -> Conversation | None:
    stmt = (
        select(Conversation)
        .where(Conversation.id == id, Conversation.user_id == user_id)
        .options(selectinload(Conversation.messages))
    )
    result = await db.execute(stmt)
    return result.scalar_one_or_none()


async def add_message(
    db: AsyncSession, conversation: Conversation, sender: str, text: str
) -> Message:
    message = Message(conversation_id=conversation.id, sender=sender, text=text)
    db.add(message)
    await db.commit()
    await db.refresh(message)
    return message


async def find_or_create(
    db: AsyncSession,
    user_id: int,
    kind: str,
    listing_id: int | None,
    roommate_profile_id: int | None,
) -> Conversation:
    stmt = select(Conversation).where(
        Conversation.user_id == user_id,
        Conversation.kind == kind,
        Conversation.listing_id == listing_id,
        Conversation.roommate_profile_id == roommate_profile_id,
    )
    existing = (await db.execute(stmt)).scalar_one_or_none()
    if existing is not None:
        await db.refresh(existing, attribute_names=["messages"])
        return existing

    title, subtitle, emoji, grad = "New chat", "", "💬", "from-violet to-mint"
    if kind == "landlord" and listing_id is not None:
        listing = await db.get(Listing, listing_id)
        if listing is not None:
            title, subtitle, emoji, grad = (
                listing.landlord_name,
                listing.title,
                "👩‍💼",
                listing.grad,
            )
    elif kind == "roommate" and roommate_profile_id is not None:
        roommate = await db.get(RoommateProfile, roommate_profile_id)
        if roommate is not None:
            title, subtitle, emoji, grad = (
                roommate.name,
                f"{roommate.school} · {roommate.year}",
                roommate.emoji,
                roommate.grad,
            )

    conversation = Conversation(
        user_id=user_id,
        kind=kind,
        listing_id=listing_id,
        roommate_profile_id=roommate_profile_id,
        title=title,
        subtitle=subtitle,
        emoji=emoji,
        grad=grad,
    )
    db.add(conversation)
    await db.commit()
    await db.refresh(conversation, attribute_names=["messages"])
    return conversation
