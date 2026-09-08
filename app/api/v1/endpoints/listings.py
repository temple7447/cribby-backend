import uuid
from datetime import datetime, timezone

from fastapi import APIRouter, Depends, File, Form, HTTPException, UploadFile, status
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm.attributes import flag_modified

from app import crud
from app.api.deps import get_current_admin_user
from app.core.storage import delete_listing_photo, upload_listing_photo
from app.db.session import get_db
from app.models.user import User
from app.schemas.listing import ListingCreate, ListingRead

router = APIRouter(prefix="/listings", tags=["listings"])


@router.get("", response_model=list[ListingRead])
async def list_listings(
    max_price: int | None = None,
    max_walk: int | None = None,
    room_type: str | None = None,
    q: str | None = None,
    near_lat: float | None = None,
    near_lng: float | None = None,
    radius_km: float | None = None,
    db: AsyncSession = Depends(get_db),
) -> list[ListingRead]:
    # Pass near_lat + near_lng together to sort results nearest-first and get a
    # distance_km on each; add radius_km to also drop anything further away.
    return await crud.listing.search(
        db,
        max_price=max_price,
        max_walk=max_walk,
        room_type=room_type,
        q=q,
        near_lat=near_lat,
        near_lng=near_lng,
        radius_km=radius_km,
    )


@router.get("/{listing_id}", response_model=ListingRead)
async def get_listing(listing_id: int, db: AsyncSession = Depends(get_db)) -> ListingRead:
    result = await crud.listing.get(db, listing_id)
    if result is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Listing not found")
    return result


# Creating and photo management are admin-only for now — listings aren't
# owned by a User account yet (landlord_name is free text), so there's no
# "is this my listing" check to make. Revisit once real landlord accounts
# exist.


@router.post("", response_model=ListingRead, status_code=status.HTTP_201_CREATED)
async def create_listing(
    payload: ListingCreate,
    db: AsyncSession = Depends(get_db),
    _admin: User = Depends(get_current_admin_user),
) -> ListingRead:
    return await crud.listing.create(db, payload.model_dump())


@router.post("/{listing_id}/photos", response_model=ListingRead)
async def upload_photo(
    listing_id: int,
    file: UploadFile = File(...),
    alt: str = Form(""),
    db: AsyncSession = Depends(get_db),
    _admin: User = Depends(get_current_admin_user),
) -> ListingRead:
    listing = await crud.listing.get(db, listing_id)
    if listing is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Listing not found")

    uploaded = await upload_listing_photo(file, listing_id)
    photo = {
        "id": str(uuid.uuid4()),
        "url": uploaded["url"],
        "public_id": uploaded["public_id"],
        "alt": alt,
        "uploaded_at": datetime.now(timezone.utc).isoformat(),
    }
    listing.photos = [*listing.photos, photo]
    flag_modified(listing, "photos")
    db.add(listing)
    await db.commit()
    await db.refresh(listing)
    return listing


@router.delete("/{listing_id}/photos/{photo_id}", response_model=ListingRead)
async def delete_photo(
    listing_id: int,
    photo_id: str,
    db: AsyncSession = Depends(get_db),
    _admin: User = Depends(get_current_admin_user),
) -> ListingRead:
    listing = await crud.listing.get(db, listing_id)
    if listing is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Listing not found")

    match = next((p for p in listing.photos if p["id"] == photo_id), None)
    if match is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Photo not found")

    delete_listing_photo(match["public_id"])
    listing.photos = [p for p in listing.photos if p["id"] != photo_id]
    flag_modified(listing, "photos")
    db.add(listing)
    await db.commit()
    await db.refresh(listing)
    return listing
