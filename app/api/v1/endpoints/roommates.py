from fastapi import APIRouter, Depends, File, HTTPException, UploadFile, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import crud
from app.api.deps import get_current_user
from app.core.storage import delete_roommate_banner, upload_roommate_banner
from app.db.session import get_db
from app.models.user import User
from app.schemas.roommate import RoommateProfileCreate, RoommateProfileRead, RoommateProfileUpdate

router = APIRouter(prefix="/roommates", tags=["roommates"])

_VISIBLE_STATES = {"live", "paused"}
_MODERATED_STATES = {"under_review", "removed"}


@router.get("", response_model=list[RoommateProfileRead])
async def list_roommates(db: AsyncSession = Depends(get_db)) -> list[RoommateProfileRead]:
    return await crud.roommate.list_live(db)


@router.get("/me", response_model=RoommateProfileRead | None)
async def get_my_roommate_profile(
    current_user: User = Depends(get_current_user), db: AsyncSession = Depends(get_db)
) -> RoommateProfileRead | None:
    return await crud.roommate.get_by_user_id(db, current_user.id)


@router.post("", response_model=RoommateProfileRead, status_code=status.HTTP_201_CREATED)
async def create_roommate_profile(
    payload: RoommateProfileCreate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> RoommateProfileRead:
    if current_user.verification_status != "verified":
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Verify your student email to create a roommate profile",
        )
    existing = await crud.roommate.get_by_user_id(db, current_user.id)
    if existing is not None:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST, detail="Profile already exists"
        )
    return await crud.roommate.create_for_user(db, payload, current_user)


@router.patch("/me", response_model=RoommateProfileRead)
async def update_my_roommate_profile(
    payload: RoommateProfileUpdate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> RoommateProfileRead:
    profile = await crud.roommate.get_by_user_id(db, current_user.id)
    if profile is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="No roommate profile yet")

    if payload.visibility is not None:
        if payload.visibility not in _VISIBLE_STATES:
            raise HTTPException(status.HTTP_400_BAD_REQUEST, "Invalid visibility value")
        if profile.visibility in _MODERATED_STATES:
            raise HTTPException(
                status.HTTP_403_FORBIDDEN,
                "This profile is under review and its visibility can't be changed",
            )

    for field, value in payload.model_dump(exclude_unset=True).items():
        setattr(profile, field, value)
    db.add(profile)
    await db.commit()
    await db.refresh(profile)
    profile.user = current_user
    return profile


@router.post("/me/banner", response_model=RoommateProfileRead)
async def upload_my_roommate_banner(
    file: UploadFile = File(...),
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> RoommateProfileRead:
    """Replaces this user's roommate post's cover photo. Self-service, no
    admin gate — a student manages only their own post's banner."""
    profile = await crud.roommate.get_by_user_id(db, current_user.id)
    if profile is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "No roommate profile yet")

    uploaded = await upload_roommate_banner(file, profile.id)
    old_public_id = profile.banner_public_id

    profile.banner_url = uploaded["url"]
    profile.banner_public_id = uploaded["public_id"]
    db.add(profile)
    await db.commit()
    await db.refresh(profile)
    profile.user = current_user

    if old_public_id:
        delete_roommate_banner(old_public_id)

    return profile


@router.delete("/me/banner", response_model=RoommateProfileRead)
async def delete_my_roommate_banner(
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> RoommateProfileRead:
    """Removes the cover photo, reverting to the emoji/gradient fallback."""
    profile = await crud.roommate.get_by_user_id(db, current_user.id)
    if profile is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "No roommate profile yet")

    old_public_id = profile.banner_public_id
    profile.banner_url = None
    profile.banner_public_id = None
    db.add(profile)
    await db.commit()
    await db.refresh(profile)
    profile.user = current_user

    if old_public_id:
        delete_roommate_banner(old_public_id)

    return profile


@router.get("/{roommate_id}", response_model=RoommateProfileRead)
async def get_roommate(roommate_id: int, db: AsyncSession = Depends(get_db)) -> RoommateProfileRead:
    result = await crud.roommate.get_with_user(db, roommate_id)
    if result is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Roommate profile not found"
        )
    return result
