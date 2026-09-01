from datetime import datetime, timedelta, timezone

from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession
from starlette.concurrency import run_in_threadpool

from app import crud
from app.api.deps import get_current_user
from app.core.config import settings
from app.core.email import send_password_reset_email, send_verification_email
from app.core.security import (
    create_access_token,
    generate_verification_code,
    hash_password,
    hash_verification_code,
)
from app.db.session import get_db
from app.models.user import User
from app.schemas.token import Token
from app.schemas.user import (
    PasswordResetConfirm,
    PasswordResetRequest,
    UserCreate,
    UserLogin,
    UserRead,
    VerifyConfirmRequest,
)

router = APIRouter(prefix="/auth", tags=["auth"])


@router.post("/signup", response_model=Token)
async def signup(payload: UserCreate, db: AsyncSession = Depends(get_db)) -> Token:
    existing = await crud.user.get_by_email(db, payload.email)
    if existing is not None:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST, detail="Email already registered"
        )
    user = await crud.user.create(db, payload)
    token = create_access_token(subject=user.email)
    return Token(access_token=token, user=user)


@router.post("/login", response_model=Token)
async def login(payload: UserLogin, db: AsyncSession = Depends(get_db)) -> Token:
    user = await crud.user.authenticate(db, payload.email, payload.password)
    if user is None:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="Incorrect email or password"
        )
    if user.is_suspended:
        # 403, not 401 — the credentials were correct, access is what's denied.
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN, detail="Your account has been suspended"
        )
    token = create_access_token(subject=user.email)
    return Token(access_token=token, user=user)


@router.post("/verify/request", status_code=status.HTTP_204_NO_CONTENT)
async def request_verification(
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> None:
    if current_user.verification_status == "verified":
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Already verified")

    now = datetime.now(timezone.utc)
    if current_user.verification_code_last_sent_at is not None:
        elapsed = (now - current_user.verification_code_last_sent_at).total_seconds()
        remaining = settings.EMAIL_VERIFICATION_RESEND_COOLDOWN_SECONDS - elapsed
        if remaining > 0:
            raise HTTPException(
                status.HTTP_429_TOO_MANY_REQUESTS,
                f"Please wait {int(remaining)}s before requesting another code",
            )

    code = generate_verification_code()
    try:
        await run_in_threadpool(send_verification_email, current_user.email, code)
    except Exception:
        raise HTTPException(
            status.HTTP_502_BAD_GATEWAY, "Couldn't send verification email — try again"
        )

    current_user.verification_code_hash = hash_verification_code(code)
    current_user.verification_code_expires_at = now + timedelta(
        minutes=settings.EMAIL_VERIFICATION_TOKEN_EXPIRE_MINUTES
    )
    current_user.verification_code_last_sent_at = now
    db.add(current_user)
    await db.commit()


@router.post("/verify/confirm", response_model=UserRead)
async def confirm_verification(
    payload: VerifyConfirmRequest,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> User:
    if current_user.verification_status == "verified":
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Already verified")
    if (
        current_user.verification_code_hash is None
        or current_user.verification_code_expires_at is None
    ):
        raise HTTPException(
            status.HTTP_400_BAD_REQUEST, "No verification code requested — request one first"
        )
    if datetime.now(timezone.utc) > current_user.verification_code_expires_at:
        raise HTTPException(
            status.HTTP_400_BAD_REQUEST, "Verification code expired — request a new one"
        )
    if hash_verification_code(payload.code.strip()) != current_user.verification_code_hash:
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Incorrect verification code")

    current_user.verification_status = "verified"
    current_user.verification_code_hash = None
    current_user.verification_code_expires_at = None
    current_user.verification_code_last_sent_at = None
    db.add(current_user)
    await db.commit()
    await db.refresh(current_user)
    return current_user


@router.post("/password-reset/request", status_code=status.HTTP_204_NO_CONTENT)
async def request_password_reset(
    payload: PasswordResetRequest, db: AsyncSession = Depends(get_db)
) -> None:
    """Unauthenticated by design — this is how you get back in when you can't
    log in. Always responds 204 regardless of whether the account exists, the
    cooldown is active, or the email send fails: surfacing any of those
    asymmetrically would let someone enumerate registered emails. Contrast
    with /verify/request, which is authenticated and already knows the
    account exists, so it can safely report failures directly."""
    user = await crud.user.get_by_email(db, payload.email)
    if user is None:
        return

    now = datetime.now(timezone.utc)
    if user.password_reset_code_last_sent_at is not None:
        elapsed = (now - user.password_reset_code_last_sent_at).total_seconds()
        if elapsed < settings.PASSWORD_RESET_RESEND_COOLDOWN_SECONDS:
            return

    code = generate_verification_code()
    try:
        await run_in_threadpool(send_password_reset_email, user.email, code)
    except Exception:
        return

    user.password_reset_code_hash = hash_verification_code(code)
    user.password_reset_code_expires_at = now + timedelta(
        minutes=settings.PASSWORD_RESET_TOKEN_EXPIRE_MINUTES
    )
    user.password_reset_code_last_sent_at = now
    db.add(user)
    await db.commit()


@router.post("/password-reset/confirm", response_model=Token)
async def confirm_password_reset(
    payload: PasswordResetConfirm, db: AsyncSession = Depends(get_db)
) -> Token:
    invalid = HTTPException(status.HTTP_400_BAD_REQUEST, "Invalid or expired code")

    user = await crud.user.get_by_email(db, payload.email)
    if (
        user is None
        or user.password_reset_code_hash is None
        or user.password_reset_code_expires_at is None
    ):
        raise invalid
    if datetime.now(timezone.utc) > user.password_reset_code_expires_at:
        raise invalid
    if hash_verification_code(payload.code.strip()) != user.password_reset_code_hash:
        raise invalid

    user.hashed_password = hash_password(payload.new_password)
    user.password_reset_code_hash = None
    user.password_reset_code_expires_at = None
    user.password_reset_code_last_sent_at = None
    db.add(user)
    await db.commit()
    await db.refresh(user)

    token = create_access_token(subject=user.email)
    return Token(access_token=token, user=user)
