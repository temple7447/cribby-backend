from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import crud
from app.api.deps import get_current_user
from app.db.session import get_db
from app.models.user import User
from app.schemas.roommate import RoommateProfileCreate, RoommateProfileRead, RoommateProfileUpdate

router = APIRouter(prefix="/roommates", tags=["roommates"])


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
    existing = await crud.roommate.get_by_user_id(db, current_user.id)
    if existing is not None:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST, detail="Profile already exists"
        )
    return await crud.roommate.create_for_user(db, payload, current_user.id)


@router.patch("/me", response_model=RoommateProfileRead)
async def update_my_roommate_profile(
    payload: RoommateProfileUpdate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> RoommateProfileRead:
    profile = await crud.roommate.get_by_user_id(db, current_user.id)
    if profile is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="No roommate profile yet")
    for field, value in payload.model_dump(exclude_unset=True).items():
        setattr(profile, field, value)
    db.add(profile)
    await db.commit()
    await db.refresh(profile)
    return profile


@router.get("/{roommate_id}", response_model=RoommateProfileRead)
async def get_roommate(roommate_id: int, db: AsyncSession = Depends(get_db)) -> RoommateProfileRead:
    result = await crud.roommate.get(db, roommate_id)
    if result is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Roommate profile not found"
        )
    return result
