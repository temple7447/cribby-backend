from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.crud.base import CRUDBase
from app.models.report import Report
from app.schemas.report import ReportCreate, ReportRead


class CRUDReport(CRUDBase[Report, ReportCreate]):
    async def create_report(
        self, db: AsyncSession, reporter_id: int, obj_in: ReportCreate
    ) -> Report:
        report = Report(reporter_id=reporter_id, **obj_in.model_dump())
        db.add(report)
        await db.commit()
        await db.refresh(report)
        return report

    async def list_by_status(self, db: AsyncSession, status: str | None = None) -> list[Report]:
        stmt = select(Report).order_by(Report.created_at.desc())
        if status is not None:
            stmt = stmt.where(Report.status == status)
        result = await db.execute(stmt)
        return list(result.scalars().all())

    async def list_filed_by(self, db: AsyncSession, reporter_id: int) -> list[Report]:
        stmt = (
            select(Report)
            .where(Report.reporter_id == reporter_id)
            .order_by(Report.created_at.desc())
        )
        result = await db.execute(stmt)
        return list(result.scalars().all())

    async def list_against_user(self, db: AsyncSession, user_id: int) -> list[Report]:
        stmt = (
            select(Report)
            .where(Report.target_type == "user", Report.target_id == user_id)
            .order_by(Report.created_at.desc())
        )
        result = await db.execute(stmt)
        return list(result.scalars().all())

    async def enrich(self, db: AsyncSession, report: Report) -> ReportRead:
        """Resolves reporter_email + a human-readable target_label so a
        moderator UI can render a report without N follow-up lookups per
        row. Best-effort — a target/reporter that no longer exists (deleted
        account, etc.) just leaves the label blank rather than failing."""
        from app import crud  # local import: avoids a crud/__init__ cycle

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


report = CRUDReport(Report)
