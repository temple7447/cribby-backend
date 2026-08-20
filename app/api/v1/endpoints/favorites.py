from fastapi import APIRouter, Depends, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.deps import get_current_user
from app.crud import favorite as favorite_crud
from app.db.session import get_db
from app.models.user import User
from app.schemas.listing import ListingRead

router = APIRouter(prefix="/favorites", tags=["favorites"])


@router.get("", response_model=list[ListingRead])
async def list_favorites(
    current_user: User = Depends(get_current_user), db: AsyncSession = Depends(get_db)
) -> list[ListingRead]:
    favorites = await favorite_crud.list_for_user(db, current_user.id)
    return [f.listing for f in favorites]


@router.put("/{listing_id}", status_code=status.HTTP_204_NO_CONTENT)
async def add_favorite(
    listing_id: int,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> None:
    await favorite_crud.add(db, current_user.id, listing_id)


@router.delete("/{listing_id}", status_code=status.HTTP_204_NO_CONTENT)
async def remove_favorite(
    listing_id: int,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> None:
    await favorite_crud.remove(db, current_user.id, listing_id)
