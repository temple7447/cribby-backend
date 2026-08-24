import logging

import resend

from app.core.config import settings

logger = logging.getLogger(__name__)


def send_verification_email(to_email: str, code: str) -> None:
    """Blocking (sync) call — the resend SDK has no async client. Callers in async
    endpoints must wrap this in starlette.concurrency.run_in_threadpool."""
    if not settings.RESEND_API_KEY:
        # warning, not info — Python's root logger drops INFO by default, and this needs
        # to be visible in local dev with zero logging setup for the console fallback to work
        logger.warning("RESEND_API_KEY not set — verification code for %s: %s", to_email, code)
        return

    resend.api_key = settings.RESEND_API_KEY
    resend.Emails.send(
        {
            "from": settings.EMAIL_FROM,
            "to": [to_email],
            "subject": "Your Cribby verification code",
            "html": (
                f"<p>Your verification code is <strong>{code}</strong>.</p>"
                f"<p>It expires in {settings.EMAIL_VERIFICATION_TOKEN_EXPIRE_MINUTES} minutes.</p>"
            ),
        }
    )
