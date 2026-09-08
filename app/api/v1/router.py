from fastapi import APIRouter

from app.api.v1.endpoints import (
    auth,
    blocks,
    bookings,
    favorites,
    listings,
    messages,
    places,
    reports,
    roommates,
    users,
)

api_router = APIRouter()
api_router.include_router(auth.router)
api_router.include_router(users.router)
api_router.include_router(listings.router)
api_router.include_router(places.router)
api_router.include_router(bookings.router)
api_router.include_router(roommates.router)
api_router.include_router(favorites.router)
api_router.include_router(messages.router)
api_router.include_router(reports.router)
api_router.include_router(blocks.router)
