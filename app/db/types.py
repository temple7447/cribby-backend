from datetime import timezone

from sqlalchemy import DateTime
from sqlalchemy.types import TypeDecorator


class UTCDateTime(TypeDecorator):
    """DateTime(timezone=True) that also works on SQLite.

    SQLite has no native timezone support, so SQLAlchemy round-trips it as a
    naive datetime — which Pydantic then serializes without a UTC marker,
    and JS's `new Date(...)` silently misreads as local time. All timestamps
    in this app are UTC (server_default=func.now()), so stamp that back on
    read.
    """

    impl = DateTime(timezone=True)
    cache_ok = True

    def process_result_value(self, value, dialect):
        if value is not None and value.tzinfo is None:
            return value.replace(tzinfo=timezone.utc)
        return value
