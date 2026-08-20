from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import crud
from app.db.session import get_db
from app.schemas.listing import ListingRead

router = APIRouter(prefix="/listings", tags=["listings"])


@router.get("", response_model=list[ListingRead])
async def list_listings(
    max_price: int | None = None,
    max_walk: int | None = None,
    room_type: str | None = None,
    q: str | None = None,
    db: AsyncSession = Depends(get_db),
) -> list[ListingRead]:
    return await crud.listing.search(
        db, max_price=max_price, max_walk=max_walk, room_type=room_type, q=q
    )


@router.get("/{listing_id}", response_model=ListingRead)
async def get_listing(listing_id: int, db: AsyncSession = Depends(get_db)) -> ListingRead:
    result = await crud.listing.get(db, listing_id)
    if result is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Listing not found")
    return result
