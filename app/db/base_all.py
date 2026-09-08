"""Import every model so Base.metadata knows about all tables — used by
Alembic autogenerate and by create_all() in dev. Nothing else should import
this module; import the models directly instead."""

from app.db.base import Base
from app.models.block import Block
from app.models.booking import Booking
from app.models.favorite import Favorite
from app.models.listing import Listing
from app.models.message import Conversation, Message
from app.models.place import Place
from app.models.report import Report
from app.models.roommate import RoommateProfile
from app.models.user import User

__all__ = [
    "Base",
    "Block",
    "Booking",
    "Favorite",
    "Listing",
    "Conversation",
    "Message",
    "Place",
    "Report",
    "RoommateProfile",
    "User",
]
