from datetime import datetime, timezone

from fastapi import APIRouter, Depends, HTTPException, Query, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import crud
from app.api.deps import get_current_admin_user, get_current_user
from app.db.session import get_db
from app.models.report import Report
from app.models.user import User
from app.schemas.report import (
    REPORT_REASONS,
    REPORT_STATUSES,
    REPORT_TARGET_TYPES,
    ReportCreate,
    ReportRead,
    ReportUpdate,
)

router = APIRouter(prefix="/reports", tags=["reports"])


async def _enrich(db: AsyncSession, report: Report) -> ReportRead:
    """Resolves reporter_email + a human-readable target_label so a moderator
    UI can render a report without N follow-up lookups per row. Best-effort —
    a target/reporter that no longer exists (deleted account, etc.) just
    leaves the label blank rather than failing the whole request."""
    read = ReportRead.model_validate(report)
    reporter = await crud.user.get(db, report.reporter_id)
    read.reporter_email = reporter.email if reporter else None

    if report.target_type == "listing":
        target = await crud.listing.get(db, report.target_id)
        read.target_label = target.title if target else None
    elif report.target_type == "roommate_profile":
        target = await crud.roommate.get(db, report.target_id)
        read.target_label = target.name if target else None
    else:  # "user"
        target = await crud.user.get(db, report.target_id)
        read.target_label = target.email if target else None
    return read


@router.post("", response_model=ReportRead, status_code=status.HTTP_201_CREATED)
async def create_report(
    payload: ReportCreate,
    current_user: User = Depends(get_current_user),
    db: AsyncSession = Depends(get_db),
) -> ReportRead:
    if payload.target_type not in REPORT_TARGET_TYPES:
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Invalid target_type")
    if payload.reason not in REPORT_REASONS:
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Invalid reason")

    if payload.target_type == "listing":
        target = await crud.listing.get(db, payload.target_id)
    elif payload.target_type == "roommate_profile":
        target = await crud.roommate.get(db, payload.target_id)
    else:  # "user"
        if payload.target_id == current_user.id:
            raise HTTPException(status.HTTP_400_BAD_REQUEST, "You can't report yourself")
        target = await crud.user.get(db, payload.target_id)
    if target is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "Report target not found")

    report = await crud.report.create_report(db, current_user.id, payload)

    if payload.target_type == "roommate_profile" and target.visibility == "live":
        target.visibility = "under_review"
        db.add(target)
        await db.commit()
    elif payload.target_type == "listing" and not target.is_hidden:
        target.is_hidden = True
        db.add(target)
        await db.commit()

    return await _enrich(db, report)


@router.get("", response_model=list[ReportRead])
async def list_reports(
    status_filter: str | None = Query(None, alias="status"),
    admin: User = Depends(get_current_admin_user),
    db: AsyncSession = Depends(get_db),
) -> list[ReportRead]:
    if status_filter is not None and status_filter not in REPORT_STATUSES:
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Invalid status")
    reports = await crud.report.list_by_status(db, status_filter)
    return [await _enrich(db, r) for r in reports]


@router.patch("/{report_id}", response_model=ReportRead)
async def update_report(
    report_id: int,
    payload: ReportUpdate,
    admin: User = Depends(get_current_admin_user),
    db: AsyncSession = Depends(get_db),
) -> ReportRead:
    if payload.status not in REPORT_STATUSES:
        raise HTTPException(status.HTTP_400_BAD_REQUEST, "Invalid status")
    report = await crud.report.get(db, report_id)
    if report is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "Report not found")

    report.status = payload.status
    if payload.status in {"resolved", "dismissed"}:
        report.resolved_at = datetime.now(timezone.utc)
        report.resolved_by_id = admin.id

    # Known limitation: if multiple reports target the same content, dismissing one
    # un-hides it even if another report on the same target is still open. Acceptable
    # at current report volume; revisit with a count-of-open-reports guard if needed.
    if report.target_type == "roommate_profile":
        profile = await crud.roommate.get(db, report.target_id)
        if profile is not None:
            if payload.status == "dismissed" and profile.visibility == "under_review":
                profile.visibility = "live"
            elif payload.status == "resolved":
                profile.visibility = "removed"
            db.add(profile)
    elif report.target_type == "listing":
        listing = await crud.listing.get(db, report.target_id)
        if listing is not None:
            if payload.status == "dismissed":
                listing.is_hidden = False
            elif payload.status == "resolved":
                listing.is_hidden = True
            db.add(listing)

    db.add(report)
    await db.commit()
    await db.refresh(report)
    return await _enrich(db, report)
