from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import crud
from app.api.deps import get_current_user
from app.db.session import get_db
from app.models.block import Block
from app.models.user import User
from app.schemas.block import BlockCreate, BlockRead

router = APIRouter(prefix="/blocks", tags=["blocks"])


@router.get("", response_model=list[BlockRead])
async def list_blocks(
    current_user: User = Depends(get_current_user), db: AsyncSession = Depends(get_db)
) -> list[Block]:
    return await crud.block.list_for_blocker(db, current_user.id)


@router.post("", response_model=BlockRead)
async def create_block(
    payload: BlockCreate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> Block:
    if payload.blocked_user_id == current_user.id:
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "You can't block yourself")
    target = await crud.user.get(db, payload.blocked_user_id)
    if target is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "User not found")
    existing = await crud.block.get_pair(db, current_user.id, payload.blocked_user_id)
    if existing is not None:
        return existing
    return await crud.block.create_block(db, current_user.id, payload.blocked_user_id)


@router.delete("/{blocked_user_id}", status_code=status.HTTP_204_NO_CONTENT)
async def delete_block(
    blocked_user_id: int,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> None:
    await crud.block.delete_pair(db, current_user.id, blocked_user_id)
