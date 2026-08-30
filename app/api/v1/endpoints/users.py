from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy import delete, select, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.deps import get_current_user
from app.core.security import verify_password
from app.db.session import get_db
from app.models.block import Block
from app.models.booking import Booking
from app.models.favorite import Favorite
from app.models.message import Conversation, Message
from app.models.report import Report
from app.models.roommate import RoommateProfile
from app.models.user import User
from app.schemas.user import AccountDeleteRequest, UserRead, UserUpdate

router = APIRouter(prefix="/users", tags=["users"])


@router.get("/me", response_model=UserRead)
async def read_me(current_user: User = Depends(get_current_user)) -> User:
    return current_user


@router.patch("/me", response_model=UserRead)
async def update_me(
    payload: UserUpdate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> User:
    for field, value in payload.model_dump(exclude_unset=True).items():
        setattr(current_user, field, value)
    db.add(current_user)
    await db.commit()
    await db.refresh(current_user)
    return current_user


@router.delete("/me", status_code=status.HTTP_204_NO_CONTENT)
async def delete_me(
    payload: AccountDeleteRequest,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> None:
    """Permanently deletes the account and everything it owns. Irreversible —
    there's no soft-delete/undo, matching what "delete my account" implies.

    No ON DELETE CASCADE exists on these foreign keys (SQLite locally doesn't
    enforce them at all; Postgres in production does), so cleanup is done
    explicitly here, in FK-safe order, inside one transaction.
    """
    if not verify_password(payload.password, current_user.hashed_password):
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "Incorrect password")

    # Conversations this user owns, plus their messages.
    convo_ids_subq = select(Conversation.id).where(Conversation.user_id == current_user.id)
    await db.execute(delete(Message).where(Message.conversation_id.in_(convo_ids_subq)))
    await db.execute(delete(Conversation).where(Conversation.user_id == current_user.id))

    # Other users' conversations may point at this user's roommate profile
    # (they messaged it) — unlink rather than delete, so their thread stays.
    result = await db.execute(
        select(RoommateProfile).where(RoommateProfile.user_id == current_user.id)
    )
    profile = result.scalar_one_or_none()
    if profile is not None:
        await db.execute(
            update(Conversation)
            .where(Conversation.roommate_profile_id == profile.id)
            .values(roommate_profile_id=None)
        )
        await db.execute(delete(RoommateProfile).where(RoommateProfile.id == profile.id))

    await db.execute(delete(Booking).where(Booking.user_id == current_user.id))
    await db.execute(delete(Favorite).where(Favorite.user_id == current_user.id))
    await db.execute(
        delete(Block).where(
            (Block.blocker_id == current_user.id) | (Block.blocked_id == current_user.id)
        )
    )

    # Preserve moderation history: keep reports this user resolved (as an
    # admin) but detach the reference; delete reports they personally filed.
    await db.execute(
        update(Report).where(Report.resolved_by_id == current_user.id).values(resolved_by_id=None)
    )
    await db.execute(delete(Report).where(Report.reporter_id == current_user.id))

    await db.delete(current_user)
    await db.commit()
