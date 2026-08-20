from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.security import hash_password, verify_password
from app.crud.base import CRUDBase
from app.models.user import User
from app.schemas.user import UserCreate


class CRUDUser(CRUDBase[User, UserCreate]):
    async def get_by_email(self, db: AsyncSession, email: str) -> User | None:
        result = await db.execute(select(User).where(User.email == email))
        return result.scalar_one_or_none()

    async def create(self, db: AsyncSession, obj_in: UserCreate) -> User:
        user = User(
            email=obj_in.email,
            hashed_password=hash_password(obj_in.password),
            name=obj_in.name,
            school=obj_in.school,
        )
        db.add(user)
        await db.commit()
        await db.refresh(user)
        return user

    async def authenticate(self, db: AsyncSession, email: str, password: str) -> User | None:
        user = await self.get_by_email(db, email)
        if user is None or not verify_password(password, user.hashed_password):
            return None
        return user


user = CRUDUser(User)
