from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")

    PROJECT_NAME: str = "Cribby API"
    API_V1_STR: str = "/api/v1"

    # Local dev defaults to SQLite (zero setup). Point DATABASE_URL at a
    # Postgres instance for production, e.g. "postgresql+asyncpg://user:pass@host/db"
    # — Render/Neon give "postgresql://", swap the driver the same way.
    DATABASE_URL: str = "sqlite+aiosqlite:///./campus.db"

    # JWT
    SECRET_KEY: str = "change-me-in-production"
    ALGORITHM: str = "HS256"
    ACCESS_TOKEN_EXPIRE_MINUTES: int = 60 * 24 * 7  # 1 week — mobile app, not a browser session

    # CORS — Expo dev server (web) + Metro default ports
    BACKEND_CORS_ORIGINS: list[str] = [
        "http://localhost:8081",
        "http://localhost:8099",
        "http://localhost:19006",
    ]


settings = Settings()
