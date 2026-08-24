from collections.abc import AsyncGenerator

from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

from app.core.config import settings

connect_args = {"check_same_thread": False} if settings.DATABASE_URL.startswith("sqlite") else {}
# pool_pre_ping: ping a pooled connection before reuse and transparently reconnect if it's
# dead — needed because Neon (and similar serverless Postgres) suspends its compute after a
# few idle minutes, which silently kills any connection SQLAlchemy was holding open.
engine = create_async_engine(
    settings.DATABASE_URL, echo=False, connect_args=connect_args, pool_pre_ping=True
)
AsyncSessionLocal = async_sessionmaker(engine, expire_on_commit=False)


async def get_db() -> AsyncGenerator[AsyncSession, None]:
    async with AsyncSessionLocal() as session:
        yield session
