from sqlalchemy import and_, or_, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.crud.base import CRUDBase
from app.models.block import Block
from app.schemas.block import BlockCreate


class CRUDBlock(CRUDBase[Block, BlockCreate]):
    async def get_pair(self, db: AsyncSession, blocker_id: int, blocked_id: int) -> Block | None:
        result = await db.execute(
            select(Block)
            .where(Block.blocker_id == blocker_id, Block.blocked_id == blocked_id)
            .options(selectinload(Block.blocked_user))
        )
        return result.scalar_one_or_none()

    async def list_for_blocker(self, db: AsyncSession, blocker_id: int) -> list[Block]:
        result = await db.execute(
            select(Block)
            .where(Block.blocker_id == blocker_id)
            .options(selectinload(Block.blocked_user))
            .order_by(Block.created_at.desc())
        )
        return list(result.scalars().all())

    async def exists_either_direction(
        self, db: AsyncSession, user_a_id: int, user_b_id: int
    ) -> bool:
        stmt = (
            select(Block.id)
            .where(
                or_(
                    and_(Block.blocker_id == user_a_id, Block.blocked_id == user_b_id),
                    and_(Block.blocker_id == user_b_id, Block.blocked_id == user_a_id),
                )
            )
            .limit(1)
        )
        return (await db.execute(stmt)).scalar_one_or_none() is not None

    async def create_block(self, db: AsyncSession, blocker_id: int, blocked_id: int) -> Block:
        block = Block(blocker_id=blocker_id, blocked_id=blocked_id)
        db.add(block)
        await db.commit()
        # re-fetch with blocked_user eager-loaded — plain refresh() won't load relationships
        result = await db.execute(
            select(Block)
            .where(Block.id == block.id)
            .options(selectinload(Block.blocked_user))
        )
        return result.scalar_one()

    async def delete_pair(self, db: AsyncSession, blocker_id: int, blocked_id: int) -> None:
        existing = await self.get_pair(db, blocker_id, blocked_id)
        if existing is not None:
            await db.delete(existing)
            await db.commit()


block = CRUDBlock(Block)
