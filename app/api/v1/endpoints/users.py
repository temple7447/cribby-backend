from fastapi import APIRouter, Depends, File, HTTPException, UploadFile, status
from sqlalchemy import delete, select, update
from sqlalchemy.ext.asyncio import AsyncSession

from app import crud
from app.api.deps import get_current_admin_user, get_current_user
from app.core.security import verify_password
from app.core.storage import delete_avatar_photo, upload_avatar_photo
from app.db.session import get_db
from app.models.block import Block
from app.models.booking import Booking
from app.models.favorite import Favorite
from app.models.message import Conversation, Message
from app.models.report import Report
from app.models.roommate import RoommateProfile
from app.models.user import User
from app.schemas.admin import AdminUserDetail
from app.schemas.user import AccountDeleteRequest, SetAdminRequest, UserRead, UserUpdate

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


@router.post("/me/avatar", response_model=UserRead)
async def upload_my_avatar(
    file: UploadFile = File(...),
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> User:
    """Replaces the profile photo. Falls back to avatar_emoji everywhere
    this is unset — uploading one just switches which the app renders."""
    uploaded = await upload_avatar_photo(file, current_user.id)
    old_public_id = current_user.avatar_public_id

    current_user.avatar_url = uploaded["url"]
    current_user.avatar_public_id = uploaded["public_id"]
    db.add(current_user)
    await db.commit()
    await db.refresh(current_user)

    if old_public_id:
        delete_avatar_photo(old_public_id)

    return current_user


@router.delete("/me/avatar", response_model=UserRead)
async def delete_my_avatar(
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> User:
    """Removes the profile photo, reverting to the avatar_emoji fallback."""
    old_public_id = current_user.avatar_public_id
    current_user.avatar_url = None
    current_user.avatar_public_id = None
    db.add(current_user)
    await db.commit()
    await db.refresh(current_user)

    if old_public_id:
        delete_avatar_photo(old_public_id)

    return current_user


@router.get("", response_model=list[UserRead])
async def list_users(
    _admin: User = Depends(get_current_admin_user),
    db: AsyncSession = Depends(get_db),
) -> list[User]:
    result = await db.execute(select(User).order_by(User.created_at.desc()))
    return list(result.scalars().all())


@router.get("/{user_id}", response_model=AdminUserDetail)
async def get_user_detail(
    user_id: int,
    _admin: User = Depends(get_current_admin_user),
    db: AsyncSession = Depends(get_db),
) -> AdminUserDetail:
    """Everything about one account in a single call — bookings, roommate
    profile, and reports both filed by and against them — so the dashboard's
    user detail page doesn't need four separate round trips."""
    target = await db.get(User, user_id)
    if target is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "User not found")

    bookings = await crud.booking.get_multi_for_user(db, user_id)

    profile_result = await db.execute(
        select(RoommateProfile).where(RoommateProfile.user_id == user_id)
    )
    profile = profile_result.scalar_one_or_none()

    filed = await crud.report.list_filed_by(db, user_id)
    against = await crud.report.list_against_user(db, user_id)

    return AdminUserDetail(
        user=target,
        bookings=bookings,
        roommate_profile=profile,
        reports_filed=[await crud.report.enrich(db, r) for r in filed],
        reports_against=[await crud.report.enrich(db, r) for r in against],
    )


@router.patch("/{user_id}/admin", response_model=UserRead)
async def set_admin(
    user_id: int,
    payload: SetAdminRequest,
    admin: User = Depends(get_current_admin_user),
    db: AsyncSession = Depends(get_db),
) -> User:
    if user_id == admin.id and not payload.is_admin:
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "You can't remove your own admin access")
    target = await db.get(User, user_id)
    if target is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "User not found")
    target.is_admin = payload.is_admin
    db.add(target)
    await db.commit()
    await db.refresh(target)
    return target
