from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import crud
from app.api.deps import get_current_admin_user
from app.db.session import get_db
from app.models.user import User
from app.schemas.place import PlaceCreate, PlaceRead

router = APIRouter(prefix="/places", tags=["places"])


@router.get("", response_model=list[PlaceRead])
async def list_places(
    category: str | None = None,
    q: str | None = None,
    near_lat: float | None = None,
    near_lng: float | None = None,
    radius_km: float | None = None,
    db: AsyncSession = Depends(get_db),
) -> list[PlaceRead]:
    """Search points of interest (hospitals, pharmacies, banks, markets, ...).

    Pass `q` and/or `category` to search by name/type. Pass `near_lat` +
    `near_lng` (usually a listing's or the campus's coordinates) to sort
    results nearest-first and get a `distance_km` on each; add `radius_km`
    to also drop anything further away.
    """
    return await crud.place.search(
        db,
        category=category,
        q=q,
        near_lat=near_lat,
        near_lng=near_lng,
        radius_km=radius_km,
    )


@router.get("/{place_id}", response_model=PlaceRead)
async def get_place(place_id: int, db: AsyncSession = Depends(get_db)) -> PlaceRead:
    result = await crud.place.get(db, place_id)
    if result is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Place not found")
    return result


# Curating the places table is admin-only — same posture as listings.
@router.post("", response_model=PlaceRead, status_code=status.HTTP_201_CREATED)
async def create_place(
    payload: PlaceCreate,
    db: AsyncSession = Depends(get_db),
    _admin: User = Depends(get_current_admin_user),
) -> PlaceRead:
    data = payload.model_dump()
    data["category"] = data["category"].lower()
    return await crud.place.create(db, data)


@router.delete("/{place_id}", status_code=status.HTTP_204_NO_CONTENT)
async def delete_place(
    place_id: int,
    db: AsyncSession = Depends(get_db),
    _admin: User = Depends(get_current_admin_user),
) -> None:
    await crud.place.delete(db, place_id)
