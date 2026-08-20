from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.deps import get_current_user
from app.crud import message as message_crud
from app.db.session import get_db
from app.models.user import User
from app.schemas.message import ConversationCreate, ConversationRead, MessageCreate, MessageRead

router = APIRouter(prefix="/conversations", tags=["messages"])


@router.get("", response_model=list[ConversationRead])
async def list_conversations(
    current_user: User = Depends(get_current_user), db: AsyncSession = Depends(get_db)
) -> list[ConversationRead]:
    return await message_crud.list_conversations_for_user(db, current_user.id)


@router.post("", response_model=ConversationRead, status_code=status.HTTP_201_CREATED)
async def start_conversation(
    payload: ConversationCreate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> ConversationRead:
    return await message_crud.find_or_create(
        db, current_user.id, payload.kind, payload.listing_id, payload.roommate_profile_id
    )


@router.get("/{conversation_id}", response_model=ConversationRead)
async def get_conversation(
    conversation_id: int,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> ConversationRead:
    conversation = await message_crud.get_conversation_for_user(
        db, conversation_id, current_user.id
    )
    if conversation is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Conversation not found")
    return conversation


@router.post(
    "/{conversation_id}/messages", response_model=MessageRead, status_code=status.HTTP_201_CREATED
)
async def send_message(
    conversation_id: int,
    payload: MessageCreate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> MessageRead:
    conversation = await message_crud.get_conversation_for_user(
        db, conversation_id, current_user.id
    )
    if conversation is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Conversation not found")
    return await message_crud.add_message(db, conversation, sender="me", text=payload.text)
