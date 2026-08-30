from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession
from starlette.concurrency import run_in_threadpool

from app import crud
from app.api.deps import get_current_admin_user, get_current_user
from app.core.email import send_booking_decision_email, send_booking_request_email
from app.db.session import get_db
from app.models.user import User
from app.schemas.booking import BookingAdminRead, BookingCreate, BookingDecision, BookingRead

router = APIRouter(prefix="/bookings", tags=["bookings"])


@router.get("", response_model=list[BookingRead])
async def list_my_bookings(
    status_filter: str | None = None,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> list[BookingRead]:
    return await crud.booking.get_multi_for_user(db, current_user.id, status=status_filter)


@router.post("", response_model=BookingRead, status_code=status.HTTP_201_CREATED)
async def create_booking(
    payload: BookingCreate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> BookingRead:
    listing = await crud.listing.get(db, payload.listing_id)
    if listing is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Listing not found")
    booking = await crud.booking.create_for_user(db, payload, current_user.id)

    # Best-effort notification — a landlord (in practice: admin, since there's
    # no real landlord-account model yet) should hear about new requests, but
    # a flaky email send must never fail the booking itself.
    try:
        for admin_email in await crud.user.list_admin_emails(db):
            await run_in_threadpool(
                send_booking_request_email,
                admin_email,
                current_user.name,
                listing.title,
                str(payload.move_in_date),
            )
    except Exception:
        pass

    return booking


# Registered before "/{booking_id}" — FastAPI matches routes in declaration
# order, and "pending" would otherwise fail int-parsing as a booking_id.
@router.get("/pending", response_model=list[BookingAdminRead])
async def list_pending_bookings(
    _admin: User = Depends(get_current_admin_user),
    db: AsyncSession = Depends(get_db),
) -> list[BookingAdminRead]:
    return await crud.booking.list_pending(db)


@router.get("/{booking_id}", response_model=BookingRead)
async def get_booking(
    booking_id: int,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> BookingRead:
    result = await crud.booking.get_for_user(db, booking_id, current_user.id)
    if result is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Booking not found")
    return result


@router.patch("/{booking_id}", response_model=BookingRead)
async def decide_booking(
    booking_id: int,
    payload: BookingDecision,
    _admin: User = Depends(get_current_admin_user),
    db: AsyncSession = Depends(get_db),
) -> BookingRead:
    booking = await crud.booking.get_with_relations(db, booking_id)
    if booking is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Booking not found")
    if booking.status != "pending":
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Booking is already {booking.status}, not pending",
        )
    decided = await crud.booking.decide(db, booking, payload.status)

    try:
        await run_in_threadpool(
            send_booking_decision_email, decided.user.email, decided.listing.title, decided.status
        )
    except Exception:
        pass

    return decided


@router.patch("/{booking_id}/cancel", response_model=BookingRead)
async def cancel_booking(
    booking_id: int,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> BookingRead:
    booking = await crud.booking.get_for_user(db, booking_id, current_user.id)
    if booking is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Booking not found")
    if booking.status in ("rejected", "canceled"):
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"Booking is already {booking.status}",
        )
    return await crud.booking.cancel(db, booking)
