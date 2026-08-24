from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.crud.base import CRUDBase
from app.models.report import Report
from app.schemas.report import ReportCreate


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


report = CRUDReport(Report)
