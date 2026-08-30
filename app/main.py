from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.api.v1.router import api_router
from app.core.config import settings

app = FastAPI(
    title=settings.PROJECT_NAME,
    openapi_url=f"{settings.API_V1_STR}/openapi.json",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=settings.BACKEND_CORS_ORIGINS,
    # Testing the Expo web build from a physical phone means the frontend is
    # loaded from the dev machine's LAN IP, not localhost — and that IP
    # changes across networks/DHCP renewals. Matching any private-network
    # address on any port covers that without needing to hardcode/update an
    # IP here each time; this only affects local dev origins, never a real
    # public one.
    allow_origin_regex=r"^http://(192\.168|10\.\d{1,3}|172\.(1[6-9]|2\d|3[01]))\.\d{1,3}\.\d{1,3}:\d+$",
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(api_router, prefix=settings.API_V1_STR)


@app.get("/health")
async def health() -> dict:
    return {"status": "ok"}
