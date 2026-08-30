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


def _send(to_email: str, subject: str, html: str) -> None:
    if not settings.RESEND_API_KEY:
        logger.warning("RESEND_API_KEY not set — email to %s not sent: %s", to_email, subject)
        return
    resend.api_key = settings.RESEND_API_KEY
    resend.Emails.send(
        {"from": settings.EMAIL_FROM, "to": [to_email], "subject": subject, "html": html}
    )


def send_booking_request_email(
    admin_email: str, student_name: str, listing_title: str, move_in_date: str
) -> None:
    """Notifies an admin (standing in for the listing's landlord — there's no
    real landlord-account model yet) that a new booking needs review."""
    _send(
        admin_email,
        f"New booking request: {listing_title}",
        (
            f"<p><strong>{student_name}</strong> requested to book "
            f"<strong>{listing_title}</strong>, move-in {move_in_date}.</p>"
            "<p>Review it in the Cribby app under Profile → Pending bookings.</p>"
        ),
    )


def send_password_reset_email(to_email: str, code: str) -> None:
    if not settings.RESEND_API_KEY:
        logger.warning("RESEND_API_KEY not set — password reset code for %s: %s", to_email, code)
        return
    _send(
        to_email,
        "Reset your Cribby password",
        (
            f"<p>Your password reset code is <strong>{code}</strong>.</p>"
            f"<p>It expires in {settings.PASSWORD_RESET_TOKEN_EXPIRE_MINUTES} minutes. "
            "If you didn't request this, you can safely ignore this email.</p>"
        ),
    )


def send_booking_decision_email(student_email: str, listing_title: str, status: str) -> None:
    verb = "approved" if status == "approved" else "rejected"
    _send(
        student_email,
        f"Your booking request was {verb}",
        f"<p>Your request for <strong>{listing_title}</strong> was <strong>{verb}</strong>.</p>"
        "<p>Check the app for details and next steps.</p>",
    )
